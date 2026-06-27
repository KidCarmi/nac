package server

import (
	"context"
	"net/http"
	"time"

	"github.com/kidcarmi/nac/pkg/version"
)

// handleAudit returns a page of audit events (admin/operator/viewer read-only).
func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	p := pageFromQuery(r)
	items, err := s.auditRepo.List(r.Context(), p)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, listResponse{Items: items, Page: pageMeta{Limit: p.Limit, Offset: p.Offset, Count: len(items)}})
}

// handleHealthz is a liveness probe: it returns 200 as long as the process can
// serve requests. It performs no dependency checks by design — liveness must
// not flap because a downstream is briefly unavailable.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, map[string]any{"status": "ok", "version": version.Get()})
}

// handleReadyz is a readiness probe: it verifies the critical dependency
// (database) is reachable so traffic is only routed to instances that can
// actually serve it.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		writeJSON(w, r, http.StatusServiceUnavailable, map[string]any{"status": "unavailable"})
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"status": "ready"})
}

// handleVersion returns build metadata.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, version.Get())
}
