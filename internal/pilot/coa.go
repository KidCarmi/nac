package pilot

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/radius"
	"github.com/kidcarmi/nac/internal/radiusauth"
)

type coaSender func(ctx context.Context, addr string, req *radius.Packet, raw, secret []byte, timeout time.Duration) (*radius.Packet, error)

func (s *Service) exchangeCoA(ctx context.Context, addr string, req *radius.Packet, raw, secret []byte, timeout time.Duration) (*radius.Packet, error) {
	resp, _, err := radius.Exchange(ctx, addr, req, raw, secret, timeout, 1)
	return resp, err
}

// coaStore is the optional conditional-update capability (PilotRepo has it).
type coaStore interface {
	UpdateCoAFrom(ctx context.Context, c *domain.CoARequest, from []string) (bool, error)
}

// RequestCoA records the operator's requested authorization for an active
// session and asks the switch, by CoA-Request, to re-authenticate it.
//
//   - quarantine: sets a session override; the re-authentication returns the
//     quarantine VLAN regardless of policy.
//   - restore: clears the override; the re-authentication returns what the
//     ACTIVE policy version decides.
//   - reauthenticate: no override change; applies the active policy version
//     (e.g. after publishing) to this session now.
//
// The override is recorded before the CoA is sent, so even if the CoA fails
// the requested state applies at the session's next authentication. The
// session is identified by switch + NAS-Port-Id + Calling-Station-Id, captured
// now; retries never re-target.
func (s *Service) RequestCoA(ctx context.Context, sessionID uuid.UUID, kind, actor string) (*domain.CoARequest, error) {
	switch kind {
	case domain.CoAQuarantine, domain.CoARestore, domain.CoAReauthenticate:
	default:
		return nil, fmt.Errorf("%w: action must be quarantine, restore or reauthenticate", domain.ErrInvalidInput)
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.State != domain.SessionActive {
		return nil, fmt.Errorf("%w: session is %s, not active; CoA can only target an authorized session on the switch", domain.ErrInvalidInput, sess.State)
	}
	sw, err := s.store.GetSwitch(ctx, sess.SwitchID)
	if err != nil {
		return nil, err
	}
	if kind == domain.CoARestore && sess.Override == "" {
		return nil, fmt.Errorf("%w: session has no quarantine override to restore; use reauthenticate to apply the active policy", domain.ErrInvalidInput)
	}
	now := s.now().UTC()

	// Requested authorization and session override.
	prevOverride := sess.Override
	switch kind {
	case domain.CoAQuarantine:
		sess.Override, sess.OverrideBy, sess.OverrideAt = domain.CoAQuarantine, actor, &now
	case domain.CoARestore:
		sess.Override, sess.OverrideBy, sess.OverrideAt = "", "", nil
	}
	seg, vlan, err := s.predict(ctx, sess, sw)
	if err != nil {
		return nil, err
	}

	nasIP := sess.NASIPAddress
	if net.ParseIP(nasIP).To4() == nil {
		nasIP = sw.NASIP
	}
	csid := sess.CallingStationID
	if csid == "" {
		csid = radiusauth.HyphenMAC(sess.MAC)
	}
	c := &domain.CoARequest{ID: uuid.New(), SessionID: sess.ID, SwitchID: sw.ID, Kind: kind, State: domain.CoARequested,
		Target: domain.CoATarget{SwitchID: sw.ID, SwitchName: sw.Name, Address: net.JoinHostPort(sw.NASIP, strconv.Itoa(sw.CoAPort)),
			NASIPAddress: nasIP, NASPortID: sess.NASPortID, CallingStationID: csid, UserName: sess.UserName,
			SessionID: sess.ID, SessionStartedAt: sess.StartedAt},
		RequestedSegment: seg, RequestedVLAN: vlan, CreatedBy: actor, CreatedAt: now, UpdatedAt: now}
	if err := s.store.InsertCoA(ctx, c); err != nil {
		return nil, err
	}
	if sess.Override != prevOverride {
		if err := s.store.UpdateSession(ctx, sess); err != nil {
			return nil, err
		}
	}
	reason := fmt.Sprintf("operator %s requested %s; target %s %s %s", actor, kind, sw.Name, sess.NASPortID, csid)
	if vlan != nil {
		reason += fmt.Sprintf("; requested segment %s VLAN %d", seg, *vlan)
	} else {
		reason += "; the active policy would reject this session at re-authentication"
	}
	s.event(ctx, &domain.AuthEvent{SessionID: &sess.ID, SwitchID: &sw.ID, MAC: sess.MAC, NASPortID: sess.NASPortID,
		Stage: domain.StageOperator, Result: "info", Source: "operator", CoAID: &c.ID, Reason: reason})
	s.audit(ctx, "session."+kind, "session", sess.ID.String(), "success", map[string]string{"coa_id": c.ID.String()})

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Detached from the HTTP request: the exchange outlives it.
		bg, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.CoAAttempts+1)*s.cfg.CoATimeout+s.cfg.CoAObserveWindow+5*time.Second)
		defer cancel()
		s.runCoA(bg, c.ID, sw)
	}()
	return c, nil
}

// predict evaluates the session's latest recorded inputs under the active
// version (honouring the override) to state the requested VLAN.
func (s *Service) predict(ctx context.Context, sess *domain.Session, sw *domain.Switch) (string, *int, error) {
	if sess.Override == domain.CoAQuarantine {
		v, ok := sw.SegmentVLANs["quarantine"]
		if !ok {
			return "", nil, fmt.Errorf("%w: switch %s has no quarantine VLAN mapping", domain.ErrInvalidInput, sw.Name)
		}
		return "quarantine", &v, nil
	}
	if sess.LastDecisionID == nil {
		return "", nil, fmt.Errorf("%w: session has no recorded decision", domain.ErrInvalidInput)
	}
	last, err := s.store.GetDecision(ctx, *sess.LastDecisionID)
	if err != nil {
		return "", nil, err
	}
	set, err := s.rules.ActiveRuleSet(ctx)
	if err != nil {
		return "", nil, err
	}
	in := domain.Inputs{}
	for k, v := range last.Inputs {
		in[k] = v
	}
	in["session.override"] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourceOperator, Note: "no operator override"}
	d := s.eval.EvaluateRuleSet(last.Subject, in, set)
	if d.Action == domain.ActionDeny {
		return "", nil, nil
	}
	v, ok := sw.SegmentVLANs[d.Segment]
	if !ok {
		return d.Segment, nil, nil
	}
	return d.Segment, &v, nil
}

func (s *Service) updateCoA(ctx context.Context, c *domain.CoARequest, from ...string) bool {
	c.UpdatedAt = s.now().UTC()
	if cs, ok := s.store.(coaStore); ok && len(from) > 0 {
		updated, err := cs.UpdateCoAFrom(ctx, c, from)
		if err != nil {
			s.logger.Error("CoA state not recorded", "error", err, "coa", c.ID)
		}
		return updated
	}
	if err := s.store.UpdateCoA(ctx, c); err != nil {
		s.logger.Error("CoA state not recorded", "error", err, "coa", c.ID)
		return false
	}
	return true
}

func (s *Service) runCoA(ctx context.Context, id uuid.UUID, sw *domain.Switch) {
	c, err := s.store.GetCoA(ctx, id)
	if err != nil {
		return
	}
	t := c.Target
	p := &radius.Packet{Code: radius.CodeCoARequest, Identifier: radius.RandomIdentifier()}
	// Attribute set as ACKed by vEOS 4.29.2F (lab/coa-radius-timeline.txt).
	if t.UserName != "" {
		p.AddString(radius.AttrUserName, t.UserName)
	}
	p.AddString(radius.AttrCallingStationID, t.CallingStationID)
	if ip := net.ParseIP(t.NASIPAddress).To4(); ip != nil {
		p.AddIPv4(radius.AttrNASIPAddress, [4]byte{ip[0], ip[1], ip[2], ip[3]})
	}
	p.AddString(radius.AttrNASPortID, t.NASPortID)
	if c.RequestedVLAN != nil {
		p.AddTunnelInt(radius.AttrTunnelType, radius.TunnelTypeVLAN)
		p.AddTunnelInt(radius.AttrTunnelMediumType, radius.TunnelMediumIEEE802)
		p.AddTunnelString(radius.AttrTunnelPrivateGroupID, strconv.Itoa(*c.RequestedVLAN))
	}
	raw, err := p.EncodeRequest([]byte(sw.RadiusSecret), false)
	if err != nil {
		s.finish(ctx, c, domain.CoAFailed, "could not encode CoA-Request: "+err.Error(), nil)
		return
	}
	rid := int(p.Identifier)
	c.RadiusID = &rid

	var resp *radius.Packet
	for attempt := 1; attempt <= s.cfg.CoAAttempts; attempt++ {
		// Never re-target: the session that was captured must still be the
		// current one, or the retry is abandoned.
		cur, gerr := s.store.GetSession(ctx, t.SessionID)
		if gerr != nil || cur.State == domain.SessionEnded || !cur.StartedAt.Equal(t.SessionStartedAt) ||
			cur.NASPortID != t.NASPortID {
			s.finish(ctx, c, domain.CoAFailed, "target session ended or changed before the CoA completed; it was not re-sent to another session", nil)
			return
		}
		now := s.now().UTC()
		c.Attempts = attempt
		if attempt == 1 {
			c.SentAt = &now
			c.State = domain.CoASent
			s.updateCoA(ctx, c, domain.CoARequested)
		} else {
			s.updateCoA(ctx, c, domain.CoASent)
		}
		resp, err = s.sendCoA(ctx, t.Address, p, raw, []byte(sw.RadiusSecret), s.cfg.CoATimeout)
		if err == nil {
			break
		}
		if !errors.Is(err, radius.ErrTimeout) {
			s.finish(ctx, c, domain.CoAFailed, "CoA transport error: "+err.Error(), nil)
			return
		}
	}
	if resp == nil {
		s.finish(ctx, c, domain.CoATimeout, fmt.Sprintf("no response from %s after %d attempt(s) of %s; check that dot1x dynamic-authorization is enabled and UDP %s is reachable",
			t.Address, c.Attempts, s.cfg.CoATimeout, t.Address), nil)
		return
	}
	cause := int(resp.Uint32(radius.AttrErrorCause))
	now := s.now().UTC()
	c.RespondedAt = &now
	if cause != 0 {
		c.ErrorCause = &cause
	}
	switch resp.Code {
	case radius.CodeCoAACK:
		s.finishACK(ctx, c, cause)
	case radius.CodeCoANAK:
		s.finish(ctx, c, domain.CoANak, "switch refused the CoA: "+radius.ErrorCauseText(uint32(cause))+
			". The requested state still applies at the session's next authentication.", &cause) //nolint:gosec // Error-Cause is a small RFC 5176 code
	default:
		s.finish(ctx, c, domain.CoAFailed, "unexpected response "+resp.Code.String(), nil)
	}
}

func (s *Service) finishACK(ctx context.Context, c *domain.CoARequest, cause int) {
	c.State = domain.CoAAcked
	c.Detail = "switch acknowledged (" + radius.ErrorCauseText(uint32(cause)) + "); waiting for the switch-initiated re-authentication" //nolint:gosec // small code
	if !s.updateCoA(ctx, c, domain.CoASent) {
		// The re-authentication was faster than this write and already
		// marked the request observed: record the response details only.
		cur, err := s.store.GetCoA(ctx, c.ID)
		if err == nil {
			cur.RespondedAt, cur.ErrorCause, cur.Attempts, cur.RadiusID = c.RespondedAt, c.ErrorCause, c.Attempts, c.RadiusID
			s.updateCoA(ctx, cur, cur.State)
		}
	}
	s.event(ctx, &domain.AuthEvent{SessionID: &c.SessionID, SwitchID: &c.SwitchID, MAC: "", NASPortID: c.Target.NASPortID,
		Stage: domain.StageCoA, Result: "info", Source: "switch", CoAID: &c.ID, Reason: "CoA-ACK received"})

	// Wait (up to the observation window) for the re-authentication; stop as
	// soon as it has been observed or the request moved on.
	deadline := time.Now().Add(s.cfg.CoAObserveWindow)
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var cur *domain.CoARequest
	for {
		var err error
		cur, err = s.store.GetCoA(ctx, c.ID)
		if err != nil || cur.State != domain.CoAAcked {
			return
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
	cur.Detail = fmt.Sprintf("switch acknowledged, but no re-authentication returned the requested VLAN within %s. "+
		"Check the session timeline and the switch (`show dot1x hosts`).", s.cfg.CoAObserveWindow)
	s.updateCoA(ctx, cur, domain.CoAAcked)
}

func (s *Service) finish(ctx context.Context, c *domain.CoARequest, state, detail string, cause *int) {
	c.State, c.Detail = state, detail
	if cause != nil && *cause != 0 {
		c.ErrorCause = cause
	}
	s.updateCoA(ctx, c, domain.CoARequested, domain.CoASent)
	result := "error"
	if state == domain.CoANak {
		result = "reject"
	}
	s.event(ctx, &domain.AuthEvent{SessionID: &c.SessionID, SwitchID: &c.SwitchID, NASPortID: c.Target.NASPortID,
		Stage: domain.StageCoA, Result: result, Source: "nacd", CoAID: &c.ID, Reason: "CoA " + state + ": " + detail})
}
