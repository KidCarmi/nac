package radiusauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/pkg/validation"
)

// Store is the persistence port for the RADIUS integration.
type Store interface {
	SwitchBySourceIP(ctx context.Context, ip string) (*domain.Switch, error)
	GetSwitch(ctx context.Context, id uuid.UUID) (*domain.Switch, error)
	TouchSwitchRadius(ctx context.Context, id uuid.UUID, at time.Time) error
	BindingByCert(ctx context.Context, issuer, serial string) (*domain.IdentityBinding, error)
	GetEndpoint(ctx context.Context, id uuid.UUID) (*domain.Endpoint, error)
	CurrentSession(ctx context.Context, switchID uuid.UUID, port, mac string) (*domain.Session, error)
	CreateSession(ctx context.Context, s *domain.Session) error
	UpdateSession(ctx context.Context, s *domain.Session) error
	GetSession(ctx context.Context, id uuid.UUID) (*domain.Session, error)
	InsertDecision(ctx context.Context, d *domain.DecisionRecord) error
	GetDecision(ctx context.Context, id uuid.UUID) (*domain.DecisionRecord, error)
	InsertEvent(ctx context.Context, e *domain.AuthEvent) (bool, error)
	// PendingCoA returns the newest CoA for the session in sent/acked state.
	PendingCoA(ctx context.Context, sessionID uuid.UUID) (*domain.CoARequest, error)
	UpdateCoAFrom(ctx context.Context, c *domain.CoARequest, from []string) (bool, error)
}

// RuleSource supplies the active policy version.
type RuleSource interface {
	ActiveRuleSet(ctx context.Context) (domain.RuleSet, error)
}

// Service implements authorize / reject / accounting.
type Service struct {
	store     Store
	rules     RuleSource
	evaluator *access.Service
	maxAge    map[string]time.Duration
	logger    *slog.Logger
	now       func() time.Time
}

// New constructs the service. evaluator supplies the engine (and its
// configured fallback); rules supplies the active version.
func New(store Store, rules RuleSource, evaluator *access.Service, postureMaxAge time.Duration, logger *slog.Logger) *Service {
	return &Service{store: store, rules: rules, evaluator: evaluator,
		maxAge: map[string]time.Duration{"posture": postureMaxAge}, logger: logger, now: time.Now}
}

// Reply is what nacd returns to FreeRADIUS.
type Reply struct {
	Accept       bool
	VLAN         int
	Segment      string
	DecisionID   uuid.UUID
	SessionID    uuid.UUID
	ReplyMessage string
}

// ErrUnknownSwitch means the packet came from an unregistered source IP.
var ErrUnknownSwitch = errors.New("switch not registered")

// Authorize decides access for a request that already passed EAP-TLS in
// FreeRADIUS. It always produces a decision record unless the switch itself
// is unknown or the request lacks the identifiers needed to key a session.
func (s *Service) Authorize(ctx context.Context, req Request) (Reply, error) {
	now := s.now().UTC()
	sw, err := s.store.SwitchBySourceIP(ctx, req.ClientIP)
	if errors.Is(err, domain.ErrNotFound) {
		s.event(ctx, &domain.AuthEvent{ClientIP: req.ClientIP, NASPortID: req.NASPortID, Stage: domain.StageDecision, Result: "reject",
			Reason: fmt.Sprintf("RADIUS client %s is not a registered switch; register it under Switches", req.ClientIP), Source: "nacd"})
		return Reply{ReplyMessage: "NAC: switch not registered"}, ErrUnknownSwitch
	}
	if err != nil {
		return Reply{}, err
	}
	_ = s.store.TouchSwitchRadius(ctx, sw.ID, now)

	mac, merr := validation.NormalizeMAC(req.CallingStationID)
	if merr != nil || req.NASPortID == "" {
		reason := "request has no valid Calling-Station-Id or NAS-Port-Id; check the switch dot1x/RADIUS configuration"
		s.event(ctx, &domain.AuthEvent{SwitchID: &sw.ID, ClientIP: req.ClientIP, NASPortID: req.NASPortID, Stage: domain.StageDecision,
			Result: "reject", Reason: reason, Source: "nacd", Detail: map[string]string{"calling_station_id": req.CallingStationID}})
		return Reply{ReplyMessage: "NAC: " + reason}, nil
	}

	serial := NormalizeSerial(req.CertSerial)
	sess, isNew, err := s.session(ctx, sw, req, mac, serial, now)
	if err != nil {
		return Reply{}, err
	}

	inputs, binding, endpoint := s.buildInputs(ctx, sw, req, mac, serial, sess, now)
	if binding != nil {
		sess.BindingID, sess.EndpointID = &binding.ID, &binding.EndpointID
	} else {
		sess.BindingID, sess.EndpointID = nil, nil
	}
	_ = endpoint

	set, err := s.rules.ActiveRuleSet(ctx)
	if err != nil {
		// No active policy is an outage of the decision service, not a deny
		// decision: surface it so FreeRADIUS applies outage semantics.
		return Reply{}, fmt.Errorf("active policy: %w", err)
	}
	d := s.evaluator.EvaluateRuleSet(subjectFor(req, mac), inputs, set)

	rec := &domain.DecisionRecord{
		ID: uuid.New(), OccurredAt: now, SessionID: &sess.ID, SwitchID: &sw.ID, Subject: d.Endpoint,
		Inputs: inputs, PolicyVersion: d.PolicyVersion, PolicyVersionID: d.PolicyVersionID,
		MatchedRule: d.MatchedRule, MatchedRuleID: d.MatchedPolicyID, Trace: d.Trace,
		Insufficient: d.InsufficientEvidence, Action: d.Action, Segment: d.Segment, Reason: d.Reason,
	}
	if rec.Insufficient == nil {
		rec.Insufficient = []string{}
	}

	// An operator session override takes precedence over policy. The policy
	// result stays in the trace so the operator can see what it would be.
	if sess.Override == domain.CoAQuarantine {
		rec.Reason = fmt.Sprintf("operator quarantine override set by %s (policy result: %s %s, %s)",
			sess.OverrideBy, d.Action, d.Segment, d.Reason)
		rec.Action, rec.Segment = domain.ActionQuarantine, "quarantine"
	}

	switch rec.Action {
	case domain.ActionAllow, domain.ActionQuarantine:
		vlan, ok := sw.SegmentVLANs[rec.Segment]
		if !ok {
			rec.Result = "reject"
			rec.Reason = fmt.Sprintf("segment %q has no VLAN mapping on switch %s; add it under Switches (%s)", rec.Segment, sw.Name, rec.Reason)
		} else {
			rec.Result, rec.VLAN = "accept", &vlan
		}
	default:
		rec.Result = "reject"
	}

	if err := s.store.InsertDecision(ctx, rec); err != nil {
		return Reply{}, err
	}
	sess.LastDecisionID = &rec.ID
	sess.RequestedAction, sess.RequestedSegment, sess.RequestedVLAN = string(rec.Action), rec.Segment, rec.VLAN
	sess.LastAuthAt = now
	if rec.Result == "accept" {
		sess.State = domain.SessionActive
	} else {
		sess.State = domain.SessionRejected
	}
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return Reply{}, err
	}

	ev := &domain.AuthEvent{SessionID: &sess.ID, SwitchID: &sw.ID, ClientIP: req.ClientIP, NASPortID: req.NASPortID, MAC: mac,
		Stage: domain.StageDecision, Result: rec.Result, Reason: rec.Reason, Source: "nacd", DecisionID: &rec.ID,
		Detail: map[string]string{"policy_version": fmt.Sprint(rec.PolicyVersion), "matched_rule": rec.MatchedRule,
			"segment": rec.Segment, "new_session": fmt.Sprint(isNew)}}
	if rec.VLAN != nil {
		ev.Detail["vlan"] = fmt.Sprint(*rec.VLAN)
	}
	s.event(ctx, ev)
	s.correlateCoA(ctx, sess, rec)

	reply := Reply{Accept: rec.Result == "accept", Segment: rec.Segment, DecisionID: rec.ID, SessionID: sess.ID}
	if rec.VLAN != nil {
		reply.VLAN = *rec.VLAN
	}
	if !reply.Accept {
		reply.ReplyMessage = "NAC: access denied (decision " + rec.ID.String() + ")"
	}
	return reply, nil
}

// session finds or creates the session for switch+port+MAC. A different
// certificate on the same port and MAC starts a new session.
func (s *Service) session(ctx context.Context, sw *domain.Switch, req Request, mac, serial string, now time.Time) (*domain.Session, bool, error) {
	cur, err := s.store.CurrentSession(ctx, sw.ID, req.NASPortID, mac)
	switch {
	// A certificate is identified by issuer AND serial (two CAs can issue the
	// same serial), exactly as identity bindings key it.
	case err == nil && cur.CertSerial != "" && serial != "" && (cur.CertSerial != serial || cur.CertIssuer != req.CertIssuer):
		cur.State, cur.EndedAt = domain.SessionEnded, &now
		if err := s.store.UpdateSession(ctx, cur); err != nil {
			return nil, false, err
		}
		s.event(ctx, &domain.AuthEvent{SessionID: &cur.ID, SwitchID: &sw.ID, MAC: mac, NASPortID: req.NASPortID, Stage: domain.StageDecision,
			Result: "info", Reason: "session ended: a different certificate authenticated on this port and MAC", Source: "nacd"})
	case err == nil:
		cur.UserName, cur.CertIssuer, cur.CertSerial, cur.CertCN = req.UserName, req.CertIssuer, serial, req.CertCN
		cur.CallingStationID, cur.NASIPAddress = req.CallingStationID, req.NASIPAddress
		if req.AcctSessionID != "" {
			cur.AcctSessionID = req.AcctSessionID
		}
		return cur, false, nil
	case !errors.Is(err, domain.ErrNotFound):
		return nil, false, err
	}
	sess := &domain.Session{ID: uuid.New(), SwitchID: sw.ID, SwitchName: sw.Name, NASPortID: req.NASPortID, MAC: mac,
		CallingStationID: req.CallingStationID, NASIPAddress: req.NASIPAddress, UserName: req.UserName,
		AcctSessionID: req.AcctSessionID, CertIssuer: req.CertIssuer, CertSerial: serial, CertCN: req.CertCN,
		State: domain.SessionRejected, StartedAt: now, LastAuthAt: now}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		if errors.Is(err, domain.ErrConflict) { // concurrent creation: use the winner
			cur, gerr := s.store.CurrentSession(ctx, sw.ID, req.NASPortID, mac)
			return cur, false, gerr
		}
		return nil, false, err
	}
	return sess, true, nil
}

// buildInputs assembles evidence. Each input is taken ONLY from its allowed
// source (domain.InputCatalog): identity from the FreeRADIUS-validated
// certificate, MAC/port from the switch, device fields and posture from the
// bound device record. Nothing in the RADIUS request can supply posture.
func (s *Service) buildInputs(ctx context.Context, sw *domain.Switch, req Request, mac, serial string, sess *domain.Session, now time.Time) (domain.Inputs, *domain.IdentityBinding, *domain.Endpoint) {
	in := domain.Inputs{}
	t := now
	at := &t

	if serial != "" && req.CertIssuer != "" {
		in.Set("identity.authenticated", "true", domain.SourceEAPTLS, at, true)
		in.Set("identity.serial", serial, domain.SourceEAPTLS, at, true)
		in.Set("identity.issuer", req.CertIssuer, domain.SourceEAPTLS, at, true)
		in.Set("identity.subject_cn", req.CertCN, domain.SourceEAPTLS, at, true)
	} else {
		for _, f := range []string{"identity.authenticated", "identity.serial", "identity.issuer", "identity.subject_cn"} {
			in[f] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourceEAPTLS, Note: "FreeRADIUS sent no validated client certificate"}
		}
	}
	in.Set("mac", mac, domain.SourceNAS, at, false)
	in.Set("nas.switch", sw.Name, domain.SourceNAS, at, false)
	in.Set("nas.port", req.NASPortID, domain.SourceNAS, at, false)
	if sess.Override != "" {
		in.Set("session.override", sess.Override, domain.SourceOperator, sess.OverrideAt, true)
	} else {
		in["session.override"] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourceOperator, Note: "no operator override"}
	}

	var binding *domain.IdentityBinding
	if serial != "" && req.CertIssuer != "" {
		b, err := s.store.BindingByCert(ctx, req.CertIssuer, serial)
		if err == nil {
			binding = b
		} else if !errors.Is(err, domain.ErrNotFound) {
			s.logger.Error("binding lookup failed", "error", err)
		}
	}
	bt := &now
	switch {
	case binding == nil:
		in["identity.bound"] = domain.Evidence{Value: "false", Status: domain.EvidencePresent, Source: domain.SourceBinding, ObservedAt: bt,
			Note: "no identity binding for this certificate issuer and serial"}
		in["mac.matches_device"] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourceBinding, Note: "no bound device to compare"}
		return in, nil, nil
	case binding.SubjectCN != "" && binding.SubjectCN != req.CertCN:
		in["identity.bound"] = domain.Evidence{Value: "false", Status: domain.EvidencePresent, Source: domain.SourceBinding, ObservedAt: bt,
			Note: fmt.Sprintf("binding %s pins CN %q but the certificate CN is %q", binding.ID, binding.SubjectCN, req.CertCN)}
		in["mac.matches_device"] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourceBinding, Note: "binding not applied"}
		return in, nil, nil
	}
	in["identity.bound"] = domain.Evidence{Value: "true", Status: domain.EvidencePresent, Source: domain.SourceBinding, ObservedAt: bt, Authenticated: true,
		Note: fmt.Sprintf("binding %s revision %d", binding.ID, binding.Revision)}

	ep, err := s.store.GetEndpoint(ctx, binding.EndpointID)
	if err != nil {
		in["mac.matches_device"] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourceBinding, Note: "bound device record not found"}
		return in, binding, nil
	}
	match := "false"
	if strings.EqualFold(ep.MAC, mac) {
		match = "true"
	}
	in["mac.matches_device"] = domain.Evidence{Value: match, Status: domain.EvidencePresent, Source: domain.SourceBinding, ObservedAt: bt,
		Note: "device MAC " + ep.MAC + ", switch reported " + mac}
	for k, v := range access.EndpointInputs(*ep) {
		if k == "mac" { // the session MAC comes from the switch, not the record
			continue
		}
		in[k] = v
	}
	if pe := in["posture"]; pe.Status == domain.EvidencePresent && pe.ObservedAt == nil {
		// Fail safe: posture whose observation time is unknown cannot be
		// shown to be fresh, so it is treated like stale evidence.
		pe.Status, pe.Note = domain.EvidenceStale, "posture has no observation time; re-report it through the device record"
		in["posture"] = pe
	}
	access.ApplyFreshness(in, now, s.maxAge)
	return in, binding, ep
}

func subjectFor(req Request, mac string) string {
	if req.CertCN != "" {
		return req.CertCN + " (" + mac + ")"
	}
	return mac
}

// correlateCoA marks a pending CoA observed when this authentication (the
// switch-initiated re-authentication) returned the VLAN it requested.
func (s *Service) correlateCoA(ctx context.Context, sess *domain.Session, rec *domain.DecisionRecord) {
	c, err := s.store.PendingCoA(ctx, sess.ID)
	if err != nil || c == nil {
		return
	}
	now := s.now().UTC()
	if rec.VLAN != nil && c.RequestedVLAN != nil && *rec.VLAN == *c.RequestedVLAN && rec.Result == "accept" {
		c.State, c.ObservedAt, c.ObservedDecisionID = domain.CoAObserved, &now, &rec.ID
		c.Detail = fmt.Sprintf("re-authentication after CoA returned VLAN %d (decision %s)", *rec.VLAN, rec.ID)
	} else {
		c.Detail = fmt.Sprintf("re-authentication after CoA returned %s/%s, not the requested VLAN", rec.Result, rec.Segment)
	}
	c.UpdatedAt = now
	if ok, err := s.store.UpdateCoAFrom(ctx, c, []string{domain.CoASent, domain.CoAAcked}); err == nil && ok {
		s.event(ctx, &domain.AuthEvent{SessionID: &sess.ID, SwitchID: &sess.SwitchID, MAC: sess.MAC, NASPortID: sess.NASPortID,
			Stage: domain.StageCoA, Result: "info", Reason: c.Detail, Source: "nacd", CoAID: &c.ID, DecisionID: &rec.ID})
	}
}

// Reject records an authentication that FreeRADIUS rejected before (or
// without) a NAC decision, typically an EAP-TLS certificate failure. The
// Module-Failure-Message carries FreeRADIUS's reason.
func (s *Service) Reject(ctx context.Context, req Request) error {
	now := s.now().UTC()
	sw, err := s.store.SwitchBySourceIP(ctx, req.ClientIP)
	var swID *uuid.UUID
	if err == nil {
		swID = &sw.ID
		_ = s.store.TouchSwitchRadius(ctx, sw.ID, now)
	}
	mac, _ := validation.NormalizeMAC(req.CallingStationID)
	reason := strings.Join(req.ModuleFailure, "; ")
	if reason == "" {
		reason = "rejected by FreeRADIUS before a NAC decision (no failure message supplied)"
	}
	ev := &domain.AuthEvent{SwitchID: swID, ClientIP: req.ClientIP, NASPortID: req.NASPortID, MAC: mac, Stage: domain.StageEAPTLS,
		Result: "reject", Reason: explainTLSFailure(reason), Source: "freeradius",
		Detail: map[string]string{"freeradius_message": reason, "user_name": req.UserName,
			"cert_serial": NormalizeSerial(req.CertSerial), "cert_issuer": req.CertIssuer, "cert_cn": req.CertCN,
			"cert_expiration": req.CertExpiration}}

	if sw != nil && mac != "" && req.NASPortID != "" {
		sess, _, err := s.session(ctx, sw, req, mac, NormalizeSerial(req.CertSerial), now)
		if err == nil {
			sess.State, sess.LastAuthAt = domain.SessionRejected, now
			sess.RequestedAction, sess.RequestedSegment, sess.RequestedVLAN = "", "", nil
			if err := s.store.UpdateSession(ctx, sess); err == nil {
				ev.SessionID = &sess.ID
			}
		}
	}
	s.event(ctx, ev)
	return nil
}

// explainTLSFailure adds an operator-language hint to common FreeRADIUS
// EAP-TLS failure messages.
func explainTLSFailure(msg string) string {
	m := strings.ToLower(msg)
	hint := ""
	switch {
	case strings.Contains(m, "unknown ca") || strings.Contains(m, "unable to get local issuer") || strings.Contains(m, "unable to get issuer"):
		hint = "the client certificate is not issued by the CA FreeRADIUS trusts (check ca_file)"
	case strings.Contains(m, "expired"):
		hint = "the client certificate has expired"
	case strings.Contains(m, "not yet valid"):
		hint = "the client certificate is not yet valid (check clocks)"
	case strings.Contains(m, "revoked"):
		hint = "the client certificate is revoked (CRL)"
	case strings.Contains(m, "unsupported certificate purpose") || strings.Contains(m, "purpose"):
		hint = "the client certificate lacks the TLS client authentication purpose"
	case strings.Contains(m, "no certificate") || strings.Contains(m, "peer did not return a certificate"):
		hint = "the supplicant sent no client certificate"
	case strings.Contains(m, "unknown eap type") || strings.Contains(m, "nak"):
		hint = "the supplicant did not agree to EAP-TLS"
	}
	if hint == "" {
		return msg
	}
	return hint + " — FreeRADIUS: " + msg
}

// Accounting records RADIUS accounting when the switch sends it (the lab
// configuration does not; sessions then end on the next login or remain
// active). Stop ends the session.
func (s *Service) Accounting(ctx context.Context, req Request) error {
	now := s.now().UTC()
	sw, err := s.store.SwitchBySourceIP(ctx, req.ClientIP)
	if err != nil {
		return err
	}
	_ = s.store.TouchSwitchRadius(ctx, sw.ID, now)
	mac, merr := validation.NormalizeMAC(req.CallingStationID)
	if merr != nil || req.NASPortID == "" {
		return nil
	}
	sess, err := s.store.CurrentSession(ctx, sw.ID, req.NASPortID, mac)
	if err != nil {
		return nil // accounting for a session we never authorized: ignore
	}
	detail := map[string]string{"acct_status_type": req.AcctStatusType, "acct_session_id": req.AcctSessionID, "class": req.Class}
	// A delayed or retransmitted packet from an EARLIER connection on the same
	// port and MAC must not touch the current session: only a Start may bind a
	// new Acct-Session-Id, and the echoed Class must name a decision of this
	// session.
	if stale := s.staleAccounting(ctx, sess, req); stale != "" {
		s.event(ctx, &domain.AuthEvent{SessionID: &sess.ID, SwitchID: &sw.ID, MAC: mac, NASPortID: req.NASPortID, Stage: domain.StageAccounting,
			Result: "info", Reason: "accounting " + req.AcctStatusType + " ignored: " + stale, Source: "switch", Detail: detail})
		return nil
	}
	if req.AcctSessionID != "" {
		sess.AcctSessionID = req.AcctSessionID
	}
	if strings.EqualFold(req.AcctStatusType, "Stop") {
		sess.State, sess.EndedAt = domain.SessionEnded, &now
	}
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return err
	}
	s.event(ctx, &domain.AuthEvent{SessionID: &sess.ID, SwitchID: &sw.ID, MAC: mac, NASPortID: req.NASPortID, Stage: domain.StageAccounting,
		Result: "info", Reason: "accounting " + req.AcctStatusType, Source: "switch", Detail: detail})
	return nil
}

// staleAccounting explains why an accounting packet does not belong to the
// current session ("" when it does).
func (s *Service) staleAccounting(ctx context.Context, sess *domain.Session, req Request) string {
	if cls := strings.TrimPrefix(req.Class, "nac:"); cls != req.Class {
		if id, err := uuid.Parse(cls); err == nil {
			d, err := s.store.GetDecision(ctx, id)
			switch {
			case errors.Is(err, domain.ErrNotFound):
				return "its Class names decision " + id.String() + ", which NAC never issued for this session"
			case err == nil && (d.SessionID == nil || *d.SessionID != sess.ID):
				return "its Class names decision " + id.String() + ", which belongs to an earlier session"
			}
		}
	}
	isStart := strings.EqualFold(req.AcctStatusType, "Start")
	if !isStart && req.AcctSessionID != "" && sess.AcctSessionID != "" && req.AcctSessionID != sess.AcctSessionID {
		return "Acct-Session-Id " + req.AcctSessionID + " is not the current session's (" + sess.AcctSessionID + ")"
	}
	return ""
}

func (s *Service) event(ctx context.Context, e *domain.AuthEvent) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = s.now().UTC()
	}
	if _, err := s.store.InsertEvent(ctx, e); err != nil {
		s.logger.Error("timeline event not recorded", "error", err, "stage", e.Stage)
	}
}
