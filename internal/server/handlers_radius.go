package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/internal/radiusauth"
)

// radiusService is the RADIUS integration used by FreeRADIUS.
type radiusService interface {
	Authorize(ctx context.Context, req radiusauth.Request) (radiusauth.Reply, error)
	Reject(ctx context.Context, req radiusauth.Request) error
	Accounting(ctx context.Context, req radiusauth.Request) error
}

// frValue builds one rlm_rest response attribute. do_xlat is always false:
// FreeRADIUS would otherwise expand %{...} sequences inside values, and some
// values (reply messages) can contain certificate-derived text.
func frValue(v string) map[string]any {
	return map[string]any{"value": []string{v}, "do_xlat": false, "op": ":="}
}

func readRadiusRequest(w http.ResponseWriter, r *http.Request) (radiusauth.Request, bool) {
	defer drainAndClose(r.Body)
	client := r.URL.Query().Get("client")
	if net.ParseIP(client) == nil {
		badRequest(w, r, "query parameter client must be the RADIUS packet source IP (FreeRADIUS: %{Packet-Src-IP-Address})")
		return radiusauth.Request{}, false
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		badRequest(w, r, "unreadable body")
		return radiusauth.Request{}, false
	}
	req, _, err := radiusauth.ParseFreeRADIUS(body, client)
	if err != nil {
		badRequest(w, r, err.Error())
		return radiusauth.Request{}, false
	}
	return req, true
}

// handleRadiusAuthorize answers FreeRADIUS post-auth. HTTP 200 always carries
// a decision (accept or reject); any 5xx means the decision service could not
// decide, which FreeRADIUS turns into the documented outage reject.
func (s *Server) handleRadiusAuthorize(w http.ResponseWriter, r *http.Request) {
	req, ok := readRadiusRequest(w, r)
	if !ok {
		return
	}
	reply, err := s.radius.Authorize(r.Context(), req)
	if err != nil && !errors.Is(err, radiusauth.ErrUnknownSwitch) {
		s.metrics.AccessDecision.WithLabelValues("error").Inc()
		writeProblem(w, r, err) // 500: FreeRADIUS treats as decision-service failure
		return
	}
	out := map[string]any{}
	if reply.Accept {
		s.metrics.AccessDecision.WithLabelValues("accept").Inc()
		out["control:Tmp-String-1"] = frValue("nac-accept")
		if reply.Role != "" {
			// Role enforcement (Aruba AOS-CX): the local role carries the
			// access policy. No Tunnel-* attributes, so the port VLAN and the
			// endpoint's IP stay unchanged.
			out["reply:Aruba-User-Role"] = frValue(reply.Role)
		} else {
			out["reply:Tunnel-Type"] = frValue("VLAN")
			out["reply:Tunnel-Medium-Type"] = frValue("IEEE-802")
			out["reply:Tunnel-Private-Group-Id"] = frValue(strconv.Itoa(reply.VLAN))
		}
		out["reply:Class"] = frValue("nac:" + reply.DecisionID.String())
	} else {
		s.metrics.AccessDecision.WithLabelValues("reject").Inc()
		out["control:Tmp-String-1"] = frValue("nac-reject")
		out["reply:Reply-Message"] = frValue(reply.ReplyMessage)
	}
	writeJSON(w, r, http.StatusOK, out)
}

// handleRadiusReject records an authentication FreeRADIUS rejected without a
// NAC accept (certificate failures, missing client certificate, ...).
func (s *Server) handleRadiusReject(w http.ResponseWriter, r *http.Request) {
	req, ok := readRadiusRequest(w, r)
	if !ok {
		return
	}
	if err := s.radius.Reject(r.Context(), req); err != nil {
		writeProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRadiusAccounting records accounting when the switch sends it.
func (s *Server) handleRadiusAccounting(w http.ResponseWriter, r *http.Request) {
	req, ok := readRadiusRequest(w, r)
	if !ok {
		return
	}
	if err := s.radius.Accounting(r.Context(), req); err != nil {
		observability.LoggerFrom(r.Context()).Warn("accounting not recorded", "error", err, "client", req.ClientIP)
		if !errors.Is(err, domain.ErrNotFound) {
			// Not persisted: answer with an error so FreeRADIUS records
			// "accounting-not-forwarded" in its outage log. FreeRADIUS still
			// answers the switch itself.
			writeProblem(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
