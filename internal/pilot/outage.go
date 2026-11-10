package pilot

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/pkg/validation"
)

// outageLine is one JSON line FreeRADIUS writes (linelog nac_outage) when it
// could not reach nacd. attrs is rlm_json json_encode output (object_simple).
type outageLine struct {
	Event    string         `json:"event"`
	UnixTime int64          `json:"unix_time"`
	Attrs    map[string]any `json:"attrs"`
}

// normalizeAttrs makes keys independent of rlm_json prefix settings
// (":User-Name", "control:Tmp-String-2" -> "User-Name", "Tmp-String-2") and
// undoes the "\/" escaping FreeRADIUS applies to values inside linelog.
func normalizeAttrs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		k = strings.TrimPrefix(k, ":")
		for _, list := range []string{"control:", "request:", "reply:"} {
			k = strings.TrimPrefix(k, list)
		}
		if s, ok := v.(string); ok {
			v = strings.ReplaceAll(s, `\/`, "/")
		}
		out[k] = v
	}
	return out
}

func attr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			return v
		case []any:
			parts := make([]string, 0, len(v))
			for _, p := range v {
				parts = append(parts, fmt.Sprint(p))
			}
			return strings.Join(parts, "; ")
		case float64:
			return fmt.Sprintf("%.0f", v)
		}
	}
	return ""
}

// ImportOutageLog imports FreeRADIUS outage lines into the timeline with
// source freeradius-outage-log. It is idempotent: each line is keyed by its
// hash, so repeated imports and restarts never duplicate entries.
func (s *Service) ImportOutageLog(ctx context.Context) StatusItem {
	now := s.now().UTC()
	st := StatusItem{At: &now}
	defer func() {
		s.mu.Lock()
		s.outageStatus = st
		s.mu.Unlock()
	}()
	if s.cfg.OutageLog == "" {
		st.State, st.Detail = "unknown", "NAC_FREERADIUS_OUTAGE_LOG is not set; outage rejects are only in the FreeRADIUS log"
		return st
	}
	f, err := os.Open(s.cfg.OutageLog)
	if errors.Is(err, os.ErrNotExist) {
		st.State, st.Detail = "yes", "no outage log yet (FreeRADIUS has not had to reject because NAC was unreachable)"
		return st
	}
	if err != nil {
		st.State, st.Detail = "no", "cannot read "+s.cfg.OutageLog+": "+err.Error()
		return st
	}
	defer func() { _ = f.Close() }()

	sws, _ := s.store.ListSwitches(ctx)
	byIP := map[string]*domain.Switch{}
	for i := range sws {
		byIP[sws[i].RadiusSourceIP] = &sws[i]
	}
	imported, bad := 0, 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ol outageLine
		if err := json.Unmarshal([]byte(line), &ol); err != nil {
			bad++
			continue
		}
		ol.Attrs = normalizeAttrs(ol.Attrs)
		sum := sha256.Sum256([]byte(line))
		at := time.Unix(ol.UnixTime, 0).UTC()
		client := attr(ol.Attrs, "Tmp-String-3", "Packet-Src-IP-Address")
		mac, _ := validation.NormalizeMAC(attr(ol.Attrs, "Calling-Station-Id"))
		reason := attr(ol.Attrs, "Tmp-String-2")
		if reason == "" {
			reason = "NAC decision service unavailable"
		}
		e := &domain.AuthEvent{ID: uuid.New(), OccurredAt: at, ClientIP: client, NASPortID: attr(ol.Attrs, "NAS-Port-Id"), MAC: mac,
			Stage: domain.StageOutage, Result: "reject", Source: "freeradius-outage-log",
			Reason: "FreeRADIUS sent Access-Reject because nacd was unreachable (" + reason + "). This is not a NAC decision.",
			Detail: map[string]string{"event": ol.Event, "cert_serial": attr(ol.Attrs, "TLS-Client-Cert-Serial"),
				"cert_cn": attr(ol.Attrs, "TLS-Client-Cert-Common-Name"), "user_name": attr(ol.Attrs, "User-Name"),
				"rest_failure": attr(ol.Attrs, "Module-Failure-Message")},
			ImportKey: "outage:" + hex.EncodeToString(sum[:16])}
		if ol.Event == "accounting-not-forwarded" {
			e.Stage, e.Result = domain.StageAccounting, "info"
			e.Reason = "FreeRADIUS answered an accounting packet but could not forward it to nacd (" + reason + ")"
		}
		if sw := byIP[client]; sw != nil {
			e.SwitchID = &sw.ID
		}
		if ok, err := s.store.InsertEvent(ctx, e); err == nil && ok {
			imported++
		}
	}
	if err := sc.Err(); err != nil {
		st.State, st.Detail = "no", "error reading outage log: "+err.Error()
		return st
	}
	st.State = "yes"
	st.Detail = fmt.Sprintf("outage log %s read; %d new entr(y/ies) imported", s.cfg.OutageLog, imported)
	if bad > 0 {
		st.Detail += fmt.Sprintf("; %d unreadable line(s) skipped", bad)
	}
	return st
}

// RadiusHealth summarises the RADIUS integration for operators.
type RadiusHealth struct {
	LastDecision     *domain.AuthEvent `json:"last_decision,omitempty"`
	LastEAPReject    *domain.AuthEvent `json:"last_eap_tls_reject,omitempty"`
	LastOutageReject *domain.AuthEvent `json:"last_outage_reject,omitempty"`
	OutageImport     StatusItem        `json:"outage_log_import"`
	ClientsFile      StatusItem        `json:"radius_client_file"`
	FreeRADIUSLoad   StatusItem        `json:"freeradius_load"`
	Semantics        string            `json:"outage_semantics"`
}

// Health returns the RADIUS integration summary.
func (s *Service) Health(ctx context.Context) (*RadiusHealth, error) {
	h := &RadiusHealth{Semantics: "No authorization cache. While nacd is unreachable FreeRADIUS rejects new logins and re-authentications " +
		"(Reply-Message \"NAC decision service unavailable\"); sessions already authorized stay up on the switch until they re-authenticate."}
	var err error
	if h.LastDecision, err = s.store.LastEvent(ctx, domain.StageDecision); err != nil {
		return nil, err
	}
	if h.LastEAPReject, err = s.store.LastEvent(ctx, domain.StageEAPTLS); err != nil {
		return nil, err
	}
	if h.LastOutageReject, err = s.store.LastEvent(ctx, domain.StageOutage); err != nil {
		return nil, err
	}
	s.mu.Lock()
	h.OutageImport, h.ClientsFile = s.outageStatus, s.clientsFileStatus
	s.mu.Unlock()
	sws, err := s.store.ListSwitches(ctx)
	if err != nil {
		return nil, err
	}
	h.FreeRADIUSLoad = s.loadState(sws)
	return h, nil
}
