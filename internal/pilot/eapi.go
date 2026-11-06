package pilot

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/pkg/validation"
)

type eapiClient interface {
	run(ctx context.Context, sw *domain.Switch, cmds ...string) ([]json.RawMessage, error)
}

type httpEAPI struct{ client *http.Client }

func newEAPIClient(insecure bool) eapiClient {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit operator opt-in (self-signed switch certificates)
	}
	return &httpEAPI{client: &http.Client{Timeout: 8 * time.Second, Transport: tr}}
}

// run executes commands through Arista eAPI (JSON-RPC runCmds).
func (e *httpEAPI) run(ctx context.Context, sw *domain.Switch, cmds ...string) ([]json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "runCmds", "id": "nac",
		"params": map[string]any{"version": 1, "cmds": cmds, "format": "json"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sw.EAPIURL+"/command-api", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(sw.EAPIUsername, sw.EAPIPassword)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("eAPI %s unreachable: %w", sw.EAPIURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errors.New("eAPI rejected the configured username/password (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("eAPI HTTP %d", resp.StatusCode)
	}
	var out struct {
		Result []json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("eAPI returned non-JSON: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("eAPI error: %s", out.Error.Message)
	}
	return out.Result, nil
}

// Dot1xHost is one supplicant from `show dot1x hosts | json`.
type Dot1xHost struct {
	Interface string
	MAC       string
	AuthStage string
	VLAN      string
	Method    string
}

// ParseDot1xHosts parses `show dot1x hosts | json` as captured on vEOS-lab
// 4.29.2F (lab/switch-session-observed.json). `show dot1x hosts detail` is
// invalid on that image.
func ParseDot1xHosts(raw []byte) ([]Dot1xHost, error) {
	var doc struct {
		Intf map[string]struct {
			Supplicants map[string]struct {
				SupplicantMac string `json:"supplicantMac"`
				AuthMethod    string `json:"authMethod"`
				AuthStage     string `json:"authStage"`
				VlanID        any    `json:"vlanId"`
			} `json:"supplicants"`
		} `json:"intfSupplicantsDict"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unexpected `show dot1x hosts` JSON: %w", err)
	}
	if doc.Intf == nil {
		return nil, errors.New("unexpected `show dot1x hosts` JSON: no intfSupplicantsDict")
	}
	var out []Dot1xHost
	for intf, v := range doc.Intf {
		for key, sup := range v.Supplicants {
			mac := sup.SupplicantMac
			if mac == "" {
				mac = key
			}
			if n, err := validation.NormalizeMAC(mac); err == nil {
				mac = n
			}
			vlan := ""
			switch t := sup.VlanID.(type) {
			case string:
				vlan = t
			case float64:
				vlan = fmt.Sprintf("%.0f", t)
			}
			out = append(out, Dot1xHost{Interface: intf, MAC: mac, AuthStage: sup.AuthStage, VLAN: vlan, Method: sup.AuthMethod})
		}
	}
	return out, nil
}

// Probe checks switch management reachability via eAPI `show version`.
func (s *Service) Probe(ctx context.Context, id uuid.UUID) (*SwitchView, error) {
	sw, err := s.store.GetSwitch(ctx, id)
	if err != nil {
		return nil, err
	}
	if sw.EAPIURL == "" {
		return &SwitchView{Switch: *sw, Status: s.status(sw)}, nil
	}
	now := s.now().UTC()
	res, err := s.eapi.run(ctx, sw, "show version")
	ok, detail := err == nil, ""
	if err != nil {
		detail = err.Error()
	} else {
		var v struct {
			Version   string `json:"version"`
			ModelName string `json:"modelName"`
		}
		if len(res) > 0 {
			_ = json.Unmarshal(res[0], &v)
		}
		detail = fmt.Sprintf("eAPI answered: %s EOS %s", v.ModelName, v.Version)
		if v.Version != "" && !strings.HasPrefix(v.Version, TestedEOSVersion) {
			detail += fmt.Sprintf(" (WARNING: pilot is validated only on EOS %s)", TestedEOSVersion)
		}
	}
	_ = s.store.RecordProbe(ctx, sw.ID, now, ok, detail)
	return s.GetSwitch(ctx, id)
}

// ObserveAll polls every switch with eAPI configured.
func (s *Service) ObserveAll(ctx context.Context) {
	sws, err := s.store.ListSwitches(ctx)
	if err != nil {
		return
	}
	for i := range sws {
		if sws[i].EAPIURL != "" {
			_ = s.observeSwitch(ctx, &sws[i])
		}
	}
}

// ObserveSwitch refreshes switch-observed state for one switch now.
func (s *Service) ObserveSwitch(ctx context.Context, id uuid.UUID) error {
	sw, err := s.store.GetSwitch(ctx, id)
	if err != nil {
		return err
	}
	if sw.EAPIURL == "" {
		return fmt.Errorf("%w: switch %s has no eAPI access configured; observation is unavailable", domain.ErrInvalidInput, sw.Name)
	}
	return s.observeSwitch(ctx, sw)
}

func (s *Service) observeSwitch(ctx context.Context, sw *domain.Switch) error {
	now := s.now().UTC()
	res, err := s.eapi.run(ctx, sw, "show dot1x hosts")
	if err != nil {
		_ = s.store.RecordProbe(ctx, sw.ID, now, false, err.Error())
		return err
	}
	_ = s.store.RecordProbe(ctx, sw.ID, now, true, "eAPI answered `show dot1x hosts`")
	if len(res) == 0 {
		return errors.New("eAPI returned no result")
	}
	hosts, err := ParseDot1xHosts(res[0])
	if err != nil {
		return err
	}
	byKey := map[string]Dot1xHost{}
	for _, h := range hosts {
		byKey[h.Interface+"|"+h.MAC] = h
	}
	sessions, err := s.store.ActiveSessionsOnSwitch(ctx, sw.ID)
	if err != nil {
		return err
	}
	for i := range sessions {
		sess := &sessions[i]
		prevStatus, prevVLAN := sess.ObservedStatus, sess.ObservedVLAN
		h, found := byKey[sess.NASPortID+"|"+sess.MAC]
		if found {
			sess.ObservedStatus, sess.ObservedVLAN = h.AuthStage, h.VLAN
			sess.ObservedDetail = "authMethod " + h.Method
		} else {
			sess.ObservedStatus, sess.ObservedVLAN, sess.ObservedDetail = "absent", "", "the switch lists no supplicant for this port and MAC"
		}
		sess.ObservedAt, sess.ObservedSource = &now, "eapi: show dot1x hosts"
		if err := s.store.UpdateSession(ctx, sess); err != nil {
			continue
		}
		if prevStatus != sess.ObservedStatus || prevVLAN != sess.ObservedVLAN {
			s.event(ctx, &domain.AuthEvent{SessionID: &sess.ID, SwitchID: &sw.ID, MAC: sess.MAC, NASPortID: sess.NASPortID,
				Stage: domain.StageObservation, Result: "info", Source: "eapi",
				Reason: fmt.Sprintf("switch reports %s VLAN %s", orDash(sess.ObservedStatus), orDash(sess.ObservedVLAN))})
		}
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Verify records that the switch completed a verified test: a NAC accept
// from this switch within the verify window and, when observation is
// available, the switch reporting SUCCESS on the requested VLAN.
func (s *Service) Verify(ctx context.Context, id uuid.UUID, actor string) (*SwitchView, error) {
	sw, err := s.store.GetSwitch(ctx, id)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	sessions, err := s.store.ActiveSessionsOnSwitch(ctx, sw.ID)
	if err != nil {
		return nil, err
	}
	var best *domain.Session
	for i := range sessions {
		if now.Sub(sessions[i].LastAuthAt) <= s.cfg.VerifyWindow && sessions[i].RequestedVLAN != nil {
			best = &sessions[i]
			break
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w: no endpoint was granted access through %s in the last %s; authenticate a test endpoint first",
			domain.ErrInvalidInput, sw.Name, s.cfg.VerifyWindow)
	}
	detail := ""
	if sw.EAPIURL != "" {
		if err := s.observeSwitch(ctx, sw); err != nil {
			return nil, fmt.Errorf("%w: switch observation failed: %w", domain.ErrInvalidInput, err)
		}
		cur, err := s.store.GetSession(ctx, best.ID)
		if err != nil {
			return nil, err
		}
		want := fmt.Sprint(*cur.RequestedVLAN)
		if !strings.EqualFold(cur.ObservedStatus, "SUCCESS") || cur.ObservedVLAN != want {
			return nil, fmt.Errorf("%w: NAC requested VLAN %s for %s on %s, but the switch reports %s VLAN %s",
				domain.ErrInvalidInput, want, cur.MAC, cur.NASPortID, orDash(cur.ObservedStatus), orDash(cur.ObservedVLAN))
		}
		detail = fmt.Sprintf("verified: %s on %s authenticated, NAC requested VLAN %s and the switch reports SUCCESS VLAN %s (eAPI)",
			cur.MAC, cur.NASPortID, want, cur.ObservedVLAN)
	} else {
		detail = fmt.Sprintf("verified (decision only): %s on %s authenticated and NAC returned VLAN %d. "+
			"Switch-side state was not observed (no eAPI access); confirm with `show dot1x hosts` on the switch.",
			best.MAC, best.NASPortID, *best.RequestedVLAN)
	}
	if err := s.store.RecordVerification(ctx, sw.ID, now, best.ID, detail); err != nil {
		return nil, err
	}
	s.audit(ctx, "switch.verify", "switch", sw.ID.String(), "success", map[string]string{"session": best.ID.String()})
	return s.GetSwitch(ctx, id)
}
