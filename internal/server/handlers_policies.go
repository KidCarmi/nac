package server

import (
	"net/http"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policy"
)

type policyRequest struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Priority    int                `json:"priority"`
	Enabled     bool               `json:"enabled"`
	Conditions  []domain.Condition `json:"conditions"`
	Action      string             `json:"action"`
	Segment     string             `json:"segment"`
}

func (req policyRequest) toInput() policy.Input {
	return policy.Input{
		Name:        req.Name,
		Description: req.Description,
		Priority:    req.Priority,
		Enabled:     req.Enabled,
		Conditions:  req.Conditions,
		Action:      domain.PolicyAction(req.Action),
		Segment:     req.Segment,
	}
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	var req policyRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid policy payload")
		return
	}
	p, err := s.policies.Create(r.Context(), req.toInput())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "policy.create", "policy", p.ID.String(), "success",
		map[string]string{"name": p.Name, "action": string(p.Action)})
	writeJSON(w, r, http.StatusCreated, p)
}

func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		badRequest(w, r, "invalid policy id")
		return
	}
	p, err := s.policies.Get(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, p)
}

func (s *Server) handleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	id, err := pathUUID(r, "id")
	if err != nil {
		badRequest(w, r, "invalid policy id")
		return
	}
	var req policyRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid policy payload")
		return
	}
	p, err := s.policies.Update(r.Context(), id, req.toInput())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "policy.update", "policy", p.ID.String(), "success", nil)
	writeJSON(w, r, http.StatusOK, p)
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		badRequest(w, r, "invalid policy id")
		return
	}
	if err := s.policies.Delete(r.Context(), id); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "policy.delete", "policy", id.String(), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	p := pageFromQuery(r)
	items, err := s.policies.List(r.Context(), p)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, listResponse{Items: items, Page: pageMeta{Limit: p.Limit, Offset: p.Offset, Count: len(items)}})
}
