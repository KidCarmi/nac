package pilot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/radiusauth"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// Requested is the authorization NAC returned (or will return).
type Requested struct {
	Action     string     `json:"action,omitempty"`
	Segment    string     `json:"segment,omitempty"`
	VLAN       *int       `json:"vlan,omitempty"`
	DecisionID *uuid.UUID `json:"decision_id,omitempty"`
	At         time.Time  `json:"at"`
	Override   string     `json:"override,omitempty"`
}

// Observed is what the switch reported, with explicit freshness.
type Observed struct {
	Freshness string     `json:"freshness"` // fresh | stale | unavailable
	Status    string     `json:"status,omitempty"`
	VLAN      string     `json:"vlan,omitempty"`
	At        *time.Time `json:"at,omitempty"`
	Source    string     `json:"source,omitempty"`
	Note      string     `json:"note"`
	// Matches is nil when it cannot be judged.
	Matches *bool `json:"matches_requested,omitempty"`
}

// SessionView is a session with requested vs observed state.
type SessionView struct {
	domain.Session
	Requested Requested          `json:"requested"`
	Observed  Observed           `json:"observed"`
	LastCoA   *domain.CoARequest `json:"last_coa,omitempty"`
	Liveness  Liveness           `json:"liveness"`
	// EndpointReachability keeps the third fact apart from switch
	// authorization (Requested) and the switch's view (Observed): whether
	// the endpoint can actually use its VLAN. NAC never measures it.
	EndpointReachability EndpointReachability `json:"endpoint_reachability"`
}

// EndpointReachability is always "not_observed": NAC knows what it asked the
// switch for and what the switch reports, not whether the endpoint has a
// usable address in that VLAN (ASTRA measured the gap: after a VLAN change
// the endpoint keeps its old DHCP lease until its next renewal).
type EndpointReachability struct {
	State string `json:"state"` // not_observed
	Note  string `json:"note"`
}

func (s *Service) view(ctx context.Context, sess domain.Session, sw *domain.Switch) SessionView {
	v := SessionView{Session: sess}
	v.Requested = Requested{Action: sess.RequestedAction, Segment: sess.RequestedSegment, VLAN: sess.RequestedVLAN,
		DecisionID: sess.LastDecisionID, At: sess.LastAuthAt, Override: sess.Override}
	now := s.now().UTC()
	switch {
	case sw != nil && sw.EAPIURL == "" && sess.ObservedAt == nil:
		v.Observed = Observed{Freshness: "unavailable", Note: "switch state is not observed: no eAPI access is configured for " + sw.Name +
			". Check `show dot1x hosts` on the switch."}
	case sess.ObservedAt == nil:
		v.Observed = Observed{Freshness: "unavailable", Note: "no switch observation recorded yet"}
	default:
		age := now.Sub(*sess.ObservedAt)
		v.Observed = Observed{Status: sess.ObservedStatus, VLAN: sess.ObservedVLAN, At: sess.ObservedAt, Source: sess.ObservedSource}
		if age > s.cfg.ObservationMaxAge {
			v.Observed.Freshness = "stale"
			v.Observed.Note = fmt.Sprintf("last observed %s ago (limit %s); the switch may have changed since", age.Truncate(time.Second), s.cfg.ObservationMaxAge)
		} else {
			v.Observed.Freshness = "fresh"
			v.Observed.Note = fmt.Sprintf("observed %s ago", age.Truncate(time.Second))
		}
		if v.Observed.Freshness == "fresh" && sess.RequestedVLAN != nil {
			m := strings.EqualFold(sess.ObservedStatus, "SUCCESS") && sess.ObservedVLAN == fmt.Sprint(*sess.RequestedVLAN)
			v.Observed.Matches = &m
		}
	}
	if coas, err := s.store.SessionCoAs(ctx, sess.ID); err == nil && len(coas) > 0 {
		v.LastCoA = &coas[len(coas)-1]
	}
	var acct *acctSeen
	if evs, err := s.store.SessionEvents(ctx, &sess); err == nil {
		acct = lastAccounting(sess.ID, evs)
	}
	v.Liveness = liveness(sess, v.Observed, acct, now)
	v.EndpointReachability = EndpointReachability{State: "not_observed", Note: reachabilityNote(v)}
	return v
}

func reachabilityNote(v SessionView) string {
	const base = "NAC does not test the endpoint. Switch authorization and the switch's VLAN do not prove a usable address: " +
		"after a VLAN change the endpoint keeps its old DHCP lease until its next renewal. Check the endpoint itself."
	if v.Observed.Matches != nil && *v.Observed.Matches && v.Observed.VLAN != "" {
		return "The switch reports this endpoint on VLAN " + v.Observed.VLAN + ", as requested. " + base
	}
	return base
}

// Liveness says whether NAC knows the endpoint is still connected. A session
// only ENDS in NAC on RADIUS accounting Stop or when a different certificate
// authenticates on the same port and MAC. Without accounting and without a
// fresh switch observation (the validated lab sends neither), "active" means
// "last authentication was accepted", not "connected now".
type Liveness struct {
	State string `json:"state"` // ended | connected | absent | unknown
	Basis string `json:"basis"`
	Note  string `json:"note"`
}

// acctSeen is the latest RADIUS accounting packet NAC accepted for a session.
type acctSeen struct {
	Type string // Start | Interim-Update | Stop (as the switch sent it)
	At   time.Time
	ID   string // Acct-Session-Id
}

// lastAccounting returns the latest ACCEPTED accounting packet recorded for
// this session (packets ignored as stale are recorded as "... ignored: ..."
// and do not count), or nil. Acct-Session-Id on the session alone is not
// evidence: it is also copied from Access-Requests.
func lastAccounting(sessionID uuid.UUID, evs []domain.AuthEvent) *acctSeen {
	var last *acctSeen
	for i := range evs {
		e := &evs[i]
		if e.Stage != domain.StageAccounting || e.SessionID == nil || *e.SessionID != sessionID {
			continue
		}
		typ := e.Detail["acct_status_type"]
		if typ == "" || e.Reason != "accounting "+typ {
			continue
		}
		if last == nil || !e.OccurredAt.Before(last.At) {
			last = &acctSeen{Type: typ, At: e.OccurredAt, ID: e.Detail["acct_session_id"]}
		}
	}
	return last
}

// liveness says whether NAC knows the endpoint is still connected. The note
// states exactly what NAC has received (ASTRA, pilot-aruba-rc3: it used to say
// "no RADIUS accounting" even when an accounting Start was stored).
func liveness(sess domain.Session, o Observed, acct *acctSeen, now time.Time) Liveness {
	switch {
	case sess.State == domain.SessionEnded:
		return Liveness{State: "ended", Basis: "nac",
			Note: "ended by RADIUS accounting Stop or replaced by a different certificate on this port and MAC"}
	case o.Freshness == "fresh" && strings.EqualFold(o.Status, "SUCCESS"):
		return Liveness{State: "connected", Basis: o.Source, Note: "the switch currently lists this MAC as authenticated on this port"}
	case o.Freshness == "fresh":
		return Liveness{State: "absent", Basis: o.Source, Note: "the switch does not list this MAC as authenticated on this port; a CoA would be refused (503)"}
	}
	coa := fmt.Sprintf("Last authenticated %s ago. A CoA still targets only this switch, port and MAC; "+
		"if the endpoint left, the switch refuses it (Error-Cause 503).", now.Sub(sess.LastAuthAt).Truncate(time.Second))
	if acct != nil {
		id := ""
		if acct.ID != "" {
			id = " (Acct-Session-Id " + acct.ID + ")"
		}
		return Liveness{State: "unknown", Basis: "accounting " + acct.Type + ", no Stop",
			Note: fmt.Sprintf("NAC received RADIUS accounting %s for this session %s ago%s and no Stop since. "+
				"That shows the switch opened (or still reported) the session; it does not prove the endpoint is connected now: "+
				"NAC learns of a disconnect only from an accounting Stop, which can be lost, and it has no fresh switch observation. ",
				acct.Type, now.Sub(acct.At).Truncate(time.Second), id) + coa}
	}
	return Liveness{State: "unknown", Basis: "last authentication only",
		Note: "NAC has received no RADIUS accounting and has no fresh switch observation for this session, so it cannot tell " +
			"whether the endpoint is still connected. " + coa}
}

// SessionQuery filters the session list.
type SessionQuery struct {
	State    string
	SwitchID *uuid.UUID
	Search   string
}

// ListSessions lists sessions with requested/observed state.
func (s *Service) ListSessions(ctx context.Context, q SessionQuery) ([]SessionView, error) {
	sessions, err := s.store.ListSessions(ctx, postgres.SessionFilter{State: q.State, SwitchID: q.SwitchID, Search: q.Search})
	if err != nil {
		return nil, err
	}
	sws := map[uuid.UUID]*domain.Switch{}
	out := make([]SessionView, 0, len(sessions))
	for _, sess := range sessions {
		sw, ok := sws[sess.SwitchID]
		if !ok {
			sw, _ = s.store.GetSwitch(ctx, sess.SwitchID)
			sws[sess.SwitchID] = sw
		}
		out = append(out, s.view(ctx, sess, sw))
	}
	return out, nil
}

// TimelineEntry is one chronological step in a session's history.
type TimelineEntry struct {
	At         time.Time  `json:"at"`
	Kind       string     `json:"kind"` // event | decision | coa
	Stage      string     `json:"stage"`
	Result     string     `json:"result"`
	Summary    string     `json:"summary"`
	Source     string     `json:"source"`
	DecisionID *uuid.UUID `json:"decision_id,omitempty"`
	CoAID      *uuid.UUID `json:"coa_id,omitempty"`
}

// SessionDetail is everything needed to explain a session.
type SessionDetail struct {
	SessionView
	Switch       *SwitchView             `json:"switch,omitempty"`
	Endpoint     *domain.Endpoint        `json:"endpoint,omitempty"`
	Binding      *domain.IdentityBinding `json:"binding,omitempty"`
	LastDecision *domain.DecisionRecord  `json:"last_decision,omitempty"`
	Decisions    []domain.DecisionRecord `json:"decisions"`
	CoAs         []domain.CoARequest     `json:"coa_requests"`
	Timeline     []TimelineEntry         `json:"timeline"`
	Explanation  []string                `json:"explanation"`
}

// SessionDetail returns the full explanation of one session.
func (s *Service) SessionDetail(ctx context.Context, id uuid.UUID) (*SessionDetail, error) {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	swv, _ := s.GetSwitch(ctx, sess.SwitchID)
	var sw *domain.Switch
	if swv != nil {
		sw = &swv.Switch
	}
	d := &SessionDetail{SessionView: s.view(ctx, *sess, sw), Switch: swv}
	if sess.EndpointID != nil {
		d.Endpoint, _ = s.store.GetEndpoint(ctx, *sess.EndpointID)
	}
	if sess.BindingID != nil {
		d.Binding, _ = s.store.GetBinding(ctx, *sess.BindingID)
	}
	if d.Decisions, err = s.store.SessionDecisions(ctx, sess.ID); err != nil {
		return nil, err
	}
	if len(d.Decisions) > 0 {
		d.LastDecision = &d.Decisions[len(d.Decisions)-1]
	}
	if d.CoAs, err = s.store.SessionCoAs(ctx, sess.ID); err != nil {
		return nil, err
	}
	events, err := s.store.SessionEvents(ctx, sess)
	if err != nil {
		return nil, err
	}
	if len(events) >= postgres.SessionEventLimit {
		d.Timeline = append(d.Timeline, TimelineEntry{At: events[0].OccurredAt, Kind: "event", Stage: "timeline", Result: "info",
			Summary: fmt.Sprintf("only the %d most recent events are shown; older events are in GET /api/v1/auth-events?mac=%s",
				postgres.SessionEventLimit, sess.MAC), Source: "nacd"})
	}
	for _, e := range events {
		d.Timeline = append(d.Timeline, TimelineEntry{At: e.OccurredAt, Kind: "event", Stage: e.Stage, Result: e.Result,
			Summary: e.Reason, Source: e.Source, DecisionID: e.DecisionID, CoAID: e.CoAID})
	}
	for _, c := range d.CoAs {
		summary := fmt.Sprintf("CoA %s → %s (attempts %d)", c.Kind, c.State, c.Attempts)
		if c.Detail != "" {
			summary += ": " + c.Detail
		}
		at := c.UpdatedAt
		d.Timeline = append(d.Timeline, TimelineEntry{At: at, Kind: "coa", Stage: domain.StageCoA, Result: c.State, Summary: summary,
			Source: "nacd", CoAID: &c.ID})
	}
	sort.SliceStable(d.Timeline, func(i, j int) bool { return d.Timeline[i].At.Before(d.Timeline[j].At) })
	d.Explanation = explain(d)
	return d, nil
}

// explain produces the operator-language answer to "why is this session in
// this state?".
func explain(d *SessionDetail) []string {
	var out []string
	switch {
	case d.CertSerial == "" && d.State == domain.SessionRejected:
		out = append(out, "No validated client certificate: FreeRADIUS rejected the EAP-TLS exchange before NAC made a decision. See the eap-tls entry in the timeline for the reason.")
	case d.CertSerial != "":
		out = append(out, fmt.Sprintf("Authenticated identity: certificate CN %q, serial %s, issuer %q (validated by FreeRADIUS EAP-TLS).", d.CertCN, d.CertSerial, d.CertIssuer))
	}
	if d.Binding != nil {
		out = append(out, fmt.Sprintf("Bound to device %s by identity binding %s (revision %d).", d.Binding.EndpointID, d.Binding.ID, d.Binding.Revision))
	} else if d.CertSerial != "" {
		out = append(out, "The certificate is not bound to any device record: identity.bound is false. Bind it from this session to grant device-based access.")
	}
	if ld := d.LastDecision; ld != nil {
		line := fmt.Sprintf("Last decision %s at %s: %s", ld.ID, ld.OccurredAt.Format(time.RFC3339), strings.ToUpper(ld.Result))
		if ld.VLAN != nil {
			line += fmt.Sprintf(" with segment %s (VLAN %d)", ld.Segment, *ld.VLAN)
		}
		line += fmt.Sprintf(", policy version %d", ld.PolicyVersion)
		if ld.MatchedRule != "" {
			line += fmt.Sprintf(", matched rule %q", ld.MatchedRule)
		} else {
			line += ", no rule matched (default action)"
		}
		out = append(out, line+". Reason: "+ld.Reason)
		if len(ld.Insufficient) > 0 {
			out = append(out, "Evidence was missing or stale for: "+strings.Join(ld.Insufficient, ", ")+"; rules that needed it were skipped.")
		}
	}
	if d.State != domain.SessionRejected {
		out = append(out, "Connection: "+d.Liveness.State+" ("+d.Liveness.Basis+"). "+d.Liveness.Note)
	}
	if d.Override != "" {
		out = append(out, fmt.Sprintf("Operator override %q set by %s; it applies at every authentication until the session is restored.", d.Override, d.OverrideBy))
	}
	switch d.Observed.Freshness {
	case "unavailable":
		out = append(out, "Switch-observed state: unavailable. "+d.Observed.Note)
	case "stale":
		out = append(out, "Switch-observed state is stale: "+d.Observed.Note)
	default:
		if d.Observed.Matches != nil && !*d.Observed.Matches {
			out = append(out, fmt.Sprintf("MISMATCH: NAC requested VLAN %v but the switch reports %s VLAN %s.", deref(d.RequestedVLAN), d.Observed.Status, d.Observed.VLAN))
		} else if d.Observed.Matches != nil {
			out = append(out, "The switch reports the requested authorization ("+d.Observed.Status+" VLAN "+d.Observed.VLAN+").")
		}
	}
	return out
}

func deref(p *int) any {
	if p == nil {
		return "-"
	}
	return *p
}

// Decision returns one decision record.
func (s *Service) Decision(ctx context.Context, id uuid.UUID) (*domain.DecisionRecord, error) {
	return s.store.GetDecision(ctx, id)
}

// Events lists timeline events across sessions (e.g. recent rejections).
func (s *Service) Events(ctx context.Context, f postgres.EventFilter) ([]domain.AuthEvent, error) {
	return s.store.ListEvents(ctx, f)
}

// ---------------------------------------------------------------- bindings

// BindingInput creates an identity binding.
type BindingInput struct {
	EndpointID uuid.UUID `json:"endpoint_id"`
	Issuer     string    `json:"issuer"`
	Serial     string    `json:"serial"`
	SubjectCN  string    `json:"subject_cn"`
	Note       string    `json:"note"`
}

// CreateBinding binds a certificate identity to a device record.
func (s *Service) CreateBinding(ctx context.Context, in BindingInput, actor string) (*domain.IdentityBinding, error) {
	serial := radiusauth.NormalizeSerial(in.Serial)
	var errs []string
	if in.EndpointID == uuid.Nil {
		errs = append(errs, "endpoint_id is required")
	}
	if strings.TrimSpace(in.Issuer) == "" || len(in.Issuer) > 1024 {
		errs = append(errs, "issuer is required: the issuer DN exactly as FreeRADIUS reports it (copy it from a session)")
	}
	if serial == "" {
		errs = append(errs, "serial must be the certificate serial in hex")
	}
	if len(in.SubjectCN) > 256 || len(in.Note) > 1024 {
		errs = append(errs, "subject_cn/note too long")
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalidInput, strings.Join(errs, "; "))
	}
	if _, err := s.store.GetEndpoint(ctx, in.EndpointID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, fmt.Errorf("%w: endpoint %s does not exist", domain.ErrInvalidInput, in.EndpointID)
		}
		return nil, err
	}
	now := s.now().UTC()
	b := &domain.IdentityBinding{ID: uuid.New(), EndpointID: in.EndpointID, Issuer: strings.TrimSpace(in.Issuer), Serial: serial,
		SubjectCN: strings.TrimSpace(in.SubjectCN), Revision: 1, Note: in.Note, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err := s.store.CreateBinding(ctx, b); err != nil {
		return nil, err
	}
	s.audit(ctx, "binding.create", "identity_binding", b.ID.String(), "success",
		map[string]string{"endpoint_id": b.EndpointID.String(), "serial": b.Serial})
	return b, nil
}

// BindFromSession binds the certificate a session authenticated with to a
// device record (pinning the CN it presented).
func (s *Service) BindFromSession(ctx context.Context, sessionID, endpointID uuid.UUID, actor string) (*domain.IdentityBinding, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	// Only the authorize path writes certificate identity, and it runs only
	// after EAP-TLS succeeded; it always records a decision. Requiring both
	// means a certificate that failed validation can never be bound.
	if sess.CertSerial == "" || sess.CertIssuer == "" || sess.LastDecisionID == nil {
		return nil, fmt.Errorf("%w: this session has no certificate validated by EAP-TLS to bind", domain.ErrInvalidInput)
	}
	return s.CreateBinding(ctx, BindingInput{EndpointID: endpointID, Issuer: sess.CertIssuer, Serial: sess.CertSerial,
		SubjectCN: sess.CertCN, Note: "bound from session " + sess.ID.String()}, actor)
}

// ListBindings lists bindings.
func (s *Service) ListBindings(ctx context.Context, endpointID *uuid.UUID) ([]domain.IdentityBinding, error) {
	return s.store.ListBindings(ctx, endpointID)
}

// DeleteBinding removes a binding; it takes effect at the next authentication.
func (s *Service) DeleteBinding(ctx context.Context, id uuid.UUID, actor string) error {
	if err := s.store.DeleteBinding(ctx, id); err != nil {
		return err
	}
	s.audit(ctx, "binding.delete", "identity_binding", id.String(), "success", nil)
	return nil
}
