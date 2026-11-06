package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
)

// policyVersionService is the subset of policyver.Service the API uses.
type policyVersionService interface {
	Draft(ctx context.Context) (*policyver.Draft, error)
	SetDraftDefaultAction(ctx context.Context, a domain.PolicyAction) error
	Preview(ctx context.Context) (*policyver.Preview, error)
	Publish(ctx context.Context, in policyver.PublishInput) (*domain.PolicyVersion, error)
	Restore(ctx context.Context, in policyver.RestoreInput) (*domain.PolicyActivation, error)
	Versions(ctx context.Context) ([]domain.PolicyVersion, error)
	Version(ctx context.Context, number int) (*domain.PolicyVersion, error)
	Activations(ctx context.Context, limit int) ([]domain.PolicyActivation, error)
}

func actor(r *http.Request) string {
	if p, ok := auth.PrincipalFrom(r.Context()); ok {
		return p.Email
	}
	return ""
}

func (s *Server) handlePolicyInputs(w http.ResponseWriter, r *http.Request) {
	out := make([]domain.InputSpec, 0, len(domain.InputCatalog)+1)
	out = append(out, domain.InputCatalog...)
	out = append(out, domain.InputSpec{Field: "attr.<key>", Sources: []string{domain.SourceEndpointRecord}, Description: "custom endpoint attribute"})
	for i := range out {
		if out[i].Field == "posture" && s.cfg.Pilot.PostureMaxAge > 0 {
			out[i].MaxAgeText = s.cfg.Pilot.PostureMaxAge.String()
		}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"inputs": out,
		"operators": []domain.MatchOperator{domain.OpEquals, domain.OpNotEqual, domain.OpIn, domain.OpExists, domain.OpAbsent}})
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	d, err := s.versions.Draft(r.Context())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, d)
}

func (s *Server) handleDraftSettings(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	var req struct {
		DefaultAction string `json:"default_action"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid settings payload")
		return
	}
	if err := s.versions.SetDraftDefaultAction(r.Context(), domain.PolicyAction(req.DefaultAction)); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "policy.draft.settings", "policy_draft", "", audit.OutcomeSuccess, map[string]string{"default_action": req.DefaultAction})
	s.handleGetDraft(w, r)
}

func (s *Server) handlePreviewDraft(w http.ResponseWriter, r *http.Request) {
	p, err := s.versions.Preview(r.Context())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, p)
}

func (s *Server) handlePublishDraft(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	var req struct {
		ExpectedDigest string `json:"expected_digest"`
		Note           string `json:"note"`
	}
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid publish payload")
		return
	}
	v, err := s.versions.Publish(r.Context(), policyver.PublishInput{ExpectedDigest: req.ExpectedDigest, Note: req.Note, Actor: actor(r)})
	if err != nil {
		s.audit.Record(r.Context(), "policy.publish", "policy_version", "", audit.OutcomeFailure, map[string]string{"error": err.Error()})
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "policy.publish", "policy_version", strconv.Itoa(v.Number), audit.OutcomeSuccess,
		map[string]string{"digest": v.Digest, "note": req.Note})
	writeJSON(w, r, http.StatusCreated, map[string]any{
		"version":   v,
		"semantics": "Applies to authentications from now on. Existing sessions keep their current authorization until they re-authenticate; use the session re-authenticate action to apply it now.",
	})
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := s.versions.Versions(r.Context())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if vs == nil {
		vs = []domain.PolicyVersion{}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"versions": vs})
}

func versionNumber(r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("number"))
	return n, err == nil && n > 0
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	n, ok := versionNumber(r)
	if !ok {
		badRequest(w, r, "version number must be a positive integer")
		return
	}
	v, err := s.versions.Version(r.Context(), n)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, v)
}

func (s *Server) handleRestoreVersion(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	n, ok := versionNumber(r)
	if !ok {
		badRequest(w, r, "version number must be a positive integer")
		return
	}
	req := struct {
		ResetDraft *bool  `json:"reset_draft"`
		Note       string `json:"note"`
	}{}
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			badRequest(w, r, "invalid restore payload")
			return
		}
	}
	reset := req.ResetDraft == nil || *req.ResetDraft
	a, err := s.versions.Restore(r.Context(), policyver.RestoreInput{Number: n, ResetDraft: reset, Note: req.Note, Actor: actor(r)})
	if err != nil {
		s.audit.Record(r.Context(), "policy.restore", "policy_version", strconv.Itoa(n), audit.OutcomeFailure, map[string]string{"error": err.Error()})
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "policy.restore", "policy_version", strconv.Itoa(n), audit.OutcomeSuccess,
		map[string]string{"reset_draft": strconv.FormatBool(reset), "note": req.Note})
	writeJSON(w, r, http.StatusOK, map[string]any{
		"activation":  a,
		"draft_reset": reset,
		"semantics":   "Applies to authentications from now on. Sessions under an operator quarantine override stay quarantined until restored individually.",
	})
}

func (s *Server) handleListActivations(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	as, err := s.versions.Activations(r.Context(), limit)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"activations": as})
}
