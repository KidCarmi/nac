package server

import (
	"net/http"

	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/pkg/validation"
)

type evaluateRequest struct {
	MAC        string            `json:"mac"`
	IP         string            `json:"ip"`
	Posture    string            `json:"posture"`
	Attributes map[string]string `json:"attributes"`
}

// handleEvaluateAccess is the NAC decision endpoint called by network
// enforcement points (switches, wireless controllers, RADIUS bridges). It is
// authenticated by a service token, not operator credentials, and is the
// hottest path in the system. The MAC is normalized and validated here before
// it ever reaches the engine.
func (s *Server) handleEvaluateAccess(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	var req evaluateRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid evaluation payload")
		return
	}

	mac, err := validation.NormalizeMAC(req.MAC)
	if err != nil {
		ve := validation.NewErrors()
		ve.Add("mac", "must be a valid MAC address")
		writeProblem(w, r, ve)
		return
	}

	posture := domain.PostureStatus(req.Posture)
	if posture != "" && !posture.Valid() {
		ve := validation.NewErrors()
		ve.Add("posture", "invalid posture status")
		writeProblem(w, r, ve)
		return
	}

	decision, err := s.access.Evaluate(r.Context(), access.EvaluateInput{
		MAC:        mac,
		IP:         req.IP,
		Posture:    posture,
		Attributes: req.Attributes,
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}

	s.metrics.AccessDecision.WithLabelValues(string(decision.Action)).Inc()
	// Access decisions are security-relevant and audited, but at a coarser
	// level than admin mutations to keep volume manageable on the hot path.
	s.audit.Record(r.Context(), "access.evaluate", "endpoint", mac, string(decision.Action),
		map[string]string{"segment": decision.Segment})
	writeJSON(w, r, http.StatusOK, decision)
}
