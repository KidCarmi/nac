package server

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/pilot"
	"github.com/kidcarmi/nac/internal/storage/postgres"
)

// pilotService is the operator workflow surface (pilot.Service).
type pilotService interface {
	CreateSwitch(ctx context.Context, in pilot.SwitchInput, actor string) (*domain.Switch, error)
	UpdateSwitch(ctx context.Context, id uuid.UUID, in pilot.SwitchInput, actor string) (*domain.Switch, error)
	DeleteSwitch(ctx context.Context, id uuid.UUID, actor string) error
	ListSwitches(ctx context.Context) ([]pilot.SwitchView, error)
	GetSwitch(ctx context.Context, id uuid.UUID) (*pilot.SwitchView, error)
	SwitchConfig(ctx context.Context, id uuid.UUID, includeSecret bool, actor string) (string, error)
	Probe(ctx context.Context, id uuid.UUID) (*pilot.SwitchView, error)
	ObserveSwitch(ctx context.Context, id uuid.UUID) error
	Verify(ctx context.Context, id uuid.UUID, actor string) (*pilot.SwitchView, error)

	CreateBinding(ctx context.Context, in pilot.BindingInput, actor string) (*domain.IdentityBinding, error)
	BindFromSession(ctx context.Context, sessionID, endpointID uuid.UUID, actor string) (*domain.IdentityBinding, error)
	ListBindings(ctx context.Context, endpointID *uuid.UUID) ([]domain.IdentityBinding, error)
	DeleteBinding(ctx context.Context, id uuid.UUID, actor string) error

	ListSessions(ctx context.Context, q pilot.SessionQuery) ([]pilot.SessionView, error)
	SessionDetail(ctx context.Context, id uuid.UUID) (*pilot.SessionDetail, error)
	Decision(ctx context.Context, id uuid.UUID) (*domain.DecisionRecord, error)
	Events(ctx context.Context, f postgres.EventFilter) ([]domain.AuthEvent, error)
	RequestCoA(ctx context.Context, sessionID uuid.UUID, kind, actor string) (*domain.CoARequest, error)
	Health(ctx context.Context) (*pilot.RadiusHealth, error)
}

func (s *Server) pilotRoutes(mux *http.ServeMux, authed func(auth.Permission, http.HandlerFunc) http.Handler) {
	mux.Handle("GET /api/v1/switches", authed(auth.PermNetworkRead, s.handleListSwitches))
	mux.Handle("POST /api/v1/switches", authed(auth.PermNetworkWrite, s.handleCreateSwitch))
	mux.Handle("GET /api/v1/switches/{id}", authed(auth.PermNetworkRead, s.handleGetSwitch))
	mux.Handle("PUT /api/v1/switches/{id}", authed(auth.PermNetworkWrite, s.handleUpdateSwitch))
	mux.Handle("DELETE /api/v1/switches/{id}", authed(auth.PermNetworkWrite, s.handleDeleteSwitch))
	mux.Handle("GET /api/v1/switches/{id}/config", authed(auth.PermNetworkRead, s.handleSwitchConfig))
	mux.Handle("POST /api/v1/switches/{id}/probe", authed(auth.PermNetworkRead, s.handleProbeSwitch))
	mux.Handle("POST /api/v1/switches/{id}/observe", authed(auth.PermNetworkRead, s.handleObserveSwitch))
	mux.Handle("POST /api/v1/switches/{id}/verify", authed(auth.PermNetworkWrite, s.handleVerifySwitch))

	mux.Handle("GET /api/v1/identity-bindings", authed(auth.PermNetworkRead, s.handleListBindings))
	mux.Handle("POST /api/v1/identity-bindings", authed(auth.PermNetworkWrite, s.handleCreateBinding))
	mux.Handle("DELETE /api/v1/identity-bindings/{id}", authed(auth.PermNetworkWrite, s.handleDeleteBinding))

	mux.Handle("GET /api/v1/sessions", authed(auth.PermNetworkRead, s.handleListSessions))
	mux.Handle("GET /api/v1/sessions/{id}", authed(auth.PermNetworkRead, s.handleGetSession))
	mux.Handle("POST /api/v1/sessions/{id}/bind", authed(auth.PermNetworkWrite, s.handleBindSession))
	mux.Handle("POST /api/v1/sessions/{id}/quarantine", authed(auth.PermSessionAct, s.coaHandler(domain.CoAQuarantine)))
	mux.Handle("POST /api/v1/sessions/{id}/restore", authed(auth.PermSessionAct, s.coaHandler(domain.CoARestore)))
	mux.Handle("POST /api/v1/sessions/{id}/reauthenticate", authed(auth.PermSessionAct, s.coaHandler(domain.CoAReauthenticate)))
	mux.Handle("GET /api/v1/decisions/{id}", authed(auth.PermNetworkRead, s.handleGetDecision))
	mux.Handle("GET /api/v1/auth-events", authed(auth.PermNetworkRead, s.handleListEvents))
	mux.Handle("GET /api/v1/radius/health", authed(auth.PermNetworkRead, s.handleRadiusHealth))
}

func (s *Server) handleListSwitches(w http.ResponseWriter, r *http.Request) {
	out, err := s.pilot.ListSwitches(r.Context())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"switches": out})
}

func decodeSwitch(w http.ResponseWriter, r *http.Request) (pilot.SwitchInput, bool) {
	defer drainAndClose(r.Body)
	var in pilot.SwitchInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, r, "invalid switch payload: "+err.Error())
		return in, false
	}
	return in, true
}

func (s *Server) handleCreateSwitch(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeSwitch(w, r)
	if !ok {
		return
	}
	sw, err := s.pilot.CreateSwitch(r.Context(), in, actor(r))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	v, _ := s.pilot.GetSwitch(r.Context(), sw.ID)
	writeJSON(w, r, http.StatusCreated, v)
}

func (s *Server) withID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := pathUUID(r, "id")
	if err != nil {
		badRequest(w, r, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleGetSwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	v, err := s.pilot.GetSwitch(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, v)
}

func (s *Server) handleUpdateSwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	in, ok := decodeSwitch(w, r)
	if !ok {
		return
	}
	if _, err := s.pilot.UpdateSwitch(r.Context(), id, in, actor(r)); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.handleGetSwitch(w, r)
}

func (s *Server) handleDeleteSwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	if err := s.pilot.DeleteSwitch(r.Context(), id, actor(r)); err != nil {
		writeProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSwitchConfig returns the EOS configuration as text. The RADIUS key is
// a placeholder unless ?include_secret=true is passed by a writer.
func (s *Server) handleSwitchConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	include := r.URL.Query().Get("include_secret") == "true"
	if include {
		if p, ok := auth.PrincipalFrom(r.Context()); !ok || !p.Can(auth.PermNetworkWrite) {
			writeProblem(w, r, domain.ErrForbidden)
			return
		}
	}
	cfg, err := s.pilot.SwitchConfig(r.Context(), id, include, actor(r))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(cfg))
}

func (s *Server) handleProbeSwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	v, err := s.pilot.Probe(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, v)
}

func (s *Server) handleObserveSwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	if err := s.pilot.ObserveSwitch(r.Context(), id); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.handleGetSwitch(w, r)
}

func (s *Server) handleVerifySwitch(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	v, err := s.pilot.Verify(r.Context(), id, actor(r))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, v)
}

func (s *Server) handleListBindings(w http.ResponseWriter, r *http.Request) {
	var ep *uuid.UUID
	if v := r.URL.Query().Get("endpoint_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			badRequest(w, r, "invalid endpoint_id")
			return
		}
		ep = &id
	}
	out, err := s.pilot.ListBindings(r.Context(), ep)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"bindings": out})
}

func (s *Server) handleCreateBinding(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	var in pilot.BindingInput
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, r, "invalid binding payload")
		return
	}
	b, err := s.pilot.CreateBinding(r.Context(), in, actor(r))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, b)
}

func (s *Server) handleDeleteBinding(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	if err := s.pilot.DeleteBinding(r.Context(), id, actor(r)); err != nil {
		writeProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	q := pilot.SessionQuery{State: r.URL.Query().Get("state"), Search: r.URL.Query().Get("q")}
	if v := r.URL.Query().Get("switch_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			badRequest(w, r, "invalid switch_id")
			return
		}
		q.SwitchID = &id
	}
	out, err := s.pilot.ListSessions(r.Context(), q)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	d, err := s.pilot.SessionDetail(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, d)
}

func (s *Server) handleBindSession(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	var in struct {
		EndpointID uuid.UUID `json:"endpoint_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		badRequest(w, r, "invalid payload: endpoint_id required")
		return
	}
	b, err := s.pilot.BindFromSession(r.Context(), id, in.EndpointID, actor(r))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, b)
}

func (s *Server) coaHandler(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.withID(w, r)
		if !ok {
			return
		}
		c, err := s.pilot.RequestCoA(r.Context(), id, kind, actor(r))
		if err != nil {
			writeProblem(w, r, err)
			return
		}
		writeJSON(w, r, http.StatusAccepted, map[string]any{
			"coa": c,
			"semantics": "Requested state is recorded now and applies at this session's next authentication. " +
				"The CoA asks the switch to re-authenticate immediately; follow the CoA state on the session (requested, sent, acked/nak/timeout, observed).",
		})
	}
}

func (s *Server) handleGetDecision(w http.ResponseWriter, r *http.Request) {
	id, ok := s.withID(w, r)
	if !ok {
		return
	}
	d, err := s.pilot.Decision(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, d)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	out, err := s.pilot.Events(r.Context(), postgres.EventFilter{Stage: q.Get("stage"), Result: q.Get("result"), MAC: q.Get("mac"), Limit: limit})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) handleRadiusHealth(w http.ResponseWriter, r *http.Request) {
	h, err := s.pilot.Health(r.Context())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, h)
}
