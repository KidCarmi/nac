package server

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/endpoint"
)

type endpointRequest struct {
	MAC          string            `json:"mac"`
	IP           string            `json:"ip"`
	Hostname     string            `json:"hostname"`
	DeviceType   string            `json:"device_type"`
	Posture      string            `json:"posture"`
	AccessStatus string            `json:"access_status"`
	Attributes   map[string]string `json:"attributes"`
}

func (s *Server) handleCreateEndpoint(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	var req endpointRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid endpoint payload")
		return
	}
	e, err := s.endpoints.Create(r.Context(), endpoint.CreateInput{
		MAC:        req.MAC,
		IP:         req.IP,
		Hostname:   req.Hostname,
		DeviceType: req.DeviceType,
		Posture:    domain.PostureStatus(req.Posture),
		Attributes: req.Attributes,
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "endpoint.create", "endpoint", e.ID.String(), "success",
		map[string]string{"mac": e.MAC})
	writeJSON(w, r, http.StatusCreated, e)
}

func (s *Server) handleGetEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		badRequest(w, r, "invalid endpoint id")
		return
	}
	e, err := s.endpoints.Get(r.Context(), id)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, e)
}

func (s *Server) handleUpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	id, err := pathUUID(r, "id")
	if err != nil {
		badRequest(w, r, "invalid endpoint id")
		return
	}
	var req endpointRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid endpoint payload")
		return
	}
	e, err := s.endpoints.Update(r.Context(), id, endpoint.UpdateInput{
		IP:           req.IP,
		Hostname:     req.Hostname,
		DeviceType:   req.DeviceType,
		Posture:      domain.PostureStatus(req.Posture),
		AccessStatus: domain.AccessStatus(req.AccessStatus),
		Attributes:   req.Attributes,
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "endpoint.update", "endpoint", e.ID.String(), "success", nil)
	writeJSON(w, r, http.StatusOK, e)
}

func (s *Server) handleDeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "id")
	if err != nil {
		badRequest(w, r, "invalid endpoint id")
		return
	}
	if err := s.endpoints.Delete(r.Context(), id); err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "endpoint.delete", "endpoint", id.String(), "success", nil)
	w.WriteHeader(http.StatusNoContent)
}

type listResponse struct {
	Items any      `json:"items"`
	Page  pageMeta `json:"page"`
}

type pageMeta struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Count  int `json:"count"`
}

func (s *Server) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	p := pageFromQuery(r)
	f := domain.EndpointFilter{
		AccessStatus: domain.AccessStatus(r.URL.Query().Get("access_status")),
		Search:       r.URL.Query().Get("q"),
	}
	items, err := s.endpoints.List(r.Context(), f, p)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, listResponse{Items: items, Page: pageMeta{Limit: p.Limit, Offset: p.Offset, Count: len(items)}})
}

// pathUUID extracts and parses a UUID path value.
func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	return uuid.Parse(r.PathValue(name))
}

// pageFromQuery parses limit/offset query params with safe defaults. Negative
// or non-numeric values fall back to defaults; the service clamps the maximum.
func pageFromQuery(r *http.Request) domain.Page {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	return domain.Page{Limit: limit, Offset: offset}
}
