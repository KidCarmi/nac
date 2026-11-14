package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/change"
	"github.com/kidcarmi/nac/pkg/problem"
)

// changeService is the review-only change-request service. Handlers only
// parse, authorize (per route) and render: every rule is in change.Service,
// which the console uses through these same endpoints.
type changeService interface {
	Create(ctx context.Context, in change.CreateInput, actor string) (*change.ChangeRequest, error)
	Get(ctx context.Context, id uuid.UUID) (*change.Detail, error)
	List(ctx context.Context, f change.ListFilter) ([]change.ChangeRequest, error)
	Revisions(ctx context.Context, id uuid.UUID) ([]change.Revision, error)
	Revision(ctx context.Context, id uuid.UUID, n int) (*change.Revision, error)
	Edit(ctx context.Context, id uuid.UUID, expected int, c change.Content, actor string) (*change.ChangeRequest, error)
	Cancel(ctx context.Context, id uuid.UUID, expected int, actor string) (*change.ChangeRequest, error)
	Validate(ctx context.Context, id uuid.UUID, revision int, actor string) (*change.ChangeRequest, *change.Validation, error)
}

func (s *Server) changeRoutes(mux *http.ServeMux, authed func(auth.Permission, http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/change-requests", authed(auth.PermPolicyRead, s.handleListChanges))
	mux.Handle("POST /api/v1/change-requests", authed(auth.PermPolicyWrite, s.handleCreateChange))
	mux.Handle("GET /api/v1/change-requests/{id}", authed(auth.PermPolicyRead, s.handleGetChange))
	mux.Handle("GET /api/v1/change-requests/{id}/revisions", authed(auth.PermPolicyRead, s.handleListChangeRevisions))
	mux.Handle("GET /api/v1/change-requests/{id}/revisions/{revision}", authed(auth.PermPolicyRead, s.handleGetChangeRevision))
	mux.Handle("PUT /api/v1/change-requests/{id}/content", authed(auth.PermPolicyWrite, s.handlePutChangeContent))
	mux.Handle("POST /api/v1/change-requests/{id}/validations", authed(auth.PermPolicyWrite, s.handleValidateChange))
	mux.Handle("POST /api/v1/change-requests/{id}/cancel", authed(auth.PermPolicyWrite, s.handleCancelChange))
}

func changeID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		badRequest(w, r, "id must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// ifMatchRevision reads the required If-Match: "<revision>" header. Every
// change-request mutation names the revision it was made against, so a
// client acting on an old view gets 409 instead of silently overwriting.
func ifMatchRevision(w http.ResponseWriter, r *http.Request) (int, bool) {
	h := strings.TrimSpace(r.Header.Get("If-Match"))
	if h == "" {
		problem.New(http.StatusPreconditionRequired, "Precondition required",
			`If-Match: "<revision>" is required: name the change-request revision this action is based on (the ETag of GET /api/v1/change-requests/{id}).`).
			WithInstance(requestIDFrom(r)).Write(w)
		return 0, false
	}
	h = strings.Trim(strings.TrimPrefix(h, "W/"), `"`)
	n, err := strconv.Atoi(h)
	if err != nil || n < 1 {
		badRequest(w, r, `If-Match must be a change-request revision, e.g. "3"`)
		return 0, false
	}
	return n, true
}

// writeChange renders the current detail with its revision as the ETag.
func (s *Server) writeChange(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) {
	d, err := s.changes.Get(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(d.Revision)))
	writeJSON(w, r, status, d)
}

func (s *Server) handleListChanges(w http.ResponseWriter, r *http.Request) {
	state := change.State(r.URL.Query().Get("state"))
	switch state {
	case "", change.StateDraft, change.StateValidated, change.StateSimulated, change.StateCancelled:
	default:
		badRequest(w, r, "state must be draft, validated, simulated or cancelled")
		return
	}
	out, err := s.changes.List(r.Context(), change.ListFilter{State: state})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"change_requests": out, "label": change.Label})
}

func (s *Server) handleCreateChange(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	var in change.CreateInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, r, "invalid change-request payload")
		return
	}
	cr, err := s.changes.Create(r.Context(), in, actor(r))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/change-requests/"+cr.ID.String())
	s.writeChange(w, r, http.StatusCreated, cr.ID)
}

func (s *Server) handleGetChange(w http.ResponseWriter, r *http.Request) {
	id, ok := changeID(w, r)
	if !ok {
		return
	}
	s.writeChange(w, r, http.StatusOK, id)
}

func (s *Server) handleListChangeRevisions(w http.ResponseWriter, r *http.Request) {
	id, ok := changeID(w, r)
	if !ok {
		return
	}
	revs, err := s.changes.Revisions(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"revisions": revs})
}

func (s *Server) handleGetChangeRevision(w http.ResponseWriter, r *http.Request) {
	id, ok := changeID(w, r)
	if !ok {
		return
	}
	n, err := strconv.Atoi(r.PathValue("revision"))
	if err != nil || n < 1 {
		badRequest(w, r, "revision must be a positive integer")
		return
	}
	rev, err := s.changes.Revision(r.Context(), id, n)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, rev)
}

func (s *Server) handlePutChangeContent(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	id, ok := changeID(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatchRevision(w, r)
	if !ok {
		return
	}
	var c change.Content
	if err := decodeJSON(r, &c); err != nil {
		badRequest(w, r, "invalid content payload: expected {rules, default_action}")
		return
	}
	if _, err := s.changes.Edit(r.Context(), id, expected, c, actor(r)); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.writeChange(w, r, http.StatusOK, id)
}

func (s *Server) handleValidateChange(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	id, ok := changeID(w, r)
	if !ok {
		return
	}
	revision, ok := ifMatchRevision(w, r)
	if !ok {
		return
	}
	_, v, err := s.changes.Validate(r.Context(), id, revision, actor(r))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	d, err := s.changes.Get(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.Itoa(d.Revision)))
	writeJSON(w, r, http.StatusCreated, map[string]any{"validation": v, "change_request": d})
}

func (s *Server) handleCancelChange(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	id, ok := changeID(w, r)
	if !ok {
		return
	}
	expected, ok := ifMatchRevision(w, r)
	if !ok {
		return
	}
	if _, err := s.changes.Cancel(r.Context(), id, expected, actor(r)); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.writeChange(w, r, http.StatusOK, id)
}
