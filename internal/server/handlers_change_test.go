package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/change"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/observability"
)

// fakeChanges records what the handlers pass to the service. The handlers
// must only parse, authorize and render.
type fakeChanges struct {
	expected int
	calls    []string
	err      error
}

func (f *fakeChanges) detail(id uuid.UUID) *change.Detail {
	return &change.Detail{ChangeRequest: change.ChangeRequest{ID: id, Revision: 3, Workflow: change.WorkflowReviewOnly, State: change.StateDraft},
		Label: change.Label}
}
func (f *fakeChanges) Create(_ context.Context, _ change.CreateInput, _ string) (*change.ChangeRequest, error) {
	f.calls = append(f.calls, "create")
	return &f.detail(uuid.New()).ChangeRequest, f.err
}
func (f *fakeChanges) Get(_ context.Context, id uuid.UUID) (*change.Detail, error) {
	return f.detail(id), nil
}
func (f *fakeChanges) List(context.Context, change.ListFilter) ([]change.ChangeRequest, error) {
	return []change.ChangeRequest{}, nil
}
func (f *fakeChanges) Revisions(context.Context, uuid.UUID) ([]change.Revision, error) {
	return []change.Revision{}, nil
}
func (f *fakeChanges) Revision(context.Context, uuid.UUID, int) (*change.Revision, error) {
	return &change.Revision{}, nil
}
func (f *fakeChanges) Edit(_ context.Context, id uuid.UUID, expected int, _ change.Content, _ string) (*change.ChangeRequest, error) {
	f.calls, f.expected = append(f.calls, "edit"), expected
	if f.err != nil {
		return nil, f.err
	}
	return &f.detail(id).ChangeRequest, nil
}
func (f *fakeChanges) Cancel(_ context.Context, id uuid.UUID, expected int, _ string) (*change.ChangeRequest, error) {
	f.calls, f.expected = append(f.calls, "cancel"), expected
	return &f.detail(id).ChangeRequest, f.err
}
func (f *fakeChanges) Validate(_ context.Context, id uuid.UUID, revision int, _ string) (*change.ChangeRequest, *change.Validation, error) {
	f.calls, f.expected = append(f.calls, "validate"), revision
	return &f.detail(id).ChangeRequest, &change.Validation{}, f.err
}

func (f *fakeChanges) Simulate(_ context.Context, id uuid.UUID, revision int, _ string) (*change.ChangeRequest, *change.Simulation, error) {
	f.calls, f.expected = append(f.calls, "simulate"), revision
	return &f.detail(id).ChangeRequest, &change.Simulation{}, f.err
}
func (f *fakeChanges) Simulation(context.Context, uuid.UUID, uuid.UUID) (*change.Simulation, error) {
	return &change.Simulation{}, nil
}
func (f *fakeChanges) InputSet(context.Context, uuid.UUID, uuid.UUID) (*change.InputSet, error) {
	return &change.InputSet{}, nil
}
func (f *fakeChanges) Reproduce(context.Context, uuid.UUID, uuid.UUID) (*change.Reproduction, error) {
	return &change.Reproduction{}, nil
}

func newChangeServer(t *testing.T, fc *fakeChanges) (*Server, func(domain.Role) string) {
	t.Helper()
	tokens, err := auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), "nac", "nac-api", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ops, auditRepo := &fakeOperators{}, &fakeAudit{}
	srv := New(Deps{
		Config:    &config.Config{Env: "development", HTTP: config.HTTPConfig{MaxBodyBytes: 1 << 20, RateLimitPerMin: 1000}},
		Logger:    observability.NewLogger("error", "json"),
		Metrics:   observability.NewMetrics(),
		Tokens:    tokens,
		Operators: ops,
		AuditRepo: auditRepo,
		Audit:     audit.NewRecorder(auditRepo),
		Changes:   fc,
	})
	return srv, func(role domain.Role) string {
		op := &domain.Operator{ID: uuid.New(), Email: string(role) + "@nac.local", Role: role}
		ops.put(op)
		tok, _, err := tokens.Issue(op)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
}

func changeReq(srv *Server, method, path, token, ifMatch, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestChangeRoutes_RBAC(t *testing.T) {
	fc := &fakeChanges{}
	srv, token := newChangeServer(t, fc)
	viewer := token(domain.RoleViewer)
	id := uuid.New().String()
	if rec := changeReq(srv, "GET", "/api/v1/change-requests", viewer, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("viewer list: %d", rec.Code)
	}
	if rec := changeReq(srv, "GET", "/api/v1/change-requests/"+id, viewer, "", ""); rec.Code != http.StatusOK || rec.Header().Get("ETag") != `"3"` {
		t.Fatalf("viewer get: %d ETag %q", rec.Code, rec.Header().Get("ETag"))
	}
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/v1/change-requests", `{"title":"t","from_draft":true}`},
		{"PUT", "/api/v1/change-requests/" + id + "/content", `{"rules":[],"default_action":"deny"}`},
		{"POST", "/api/v1/change-requests/" + id + "/validations", ""},
		{"POST", "/api/v1/change-requests/" + id + "/cancel", ""},
		{"POST", "/api/v1/change-requests/" + id + "/simulations", ""},
	} {
		if rec := changeReq(srv, c.method, c.path, viewer, `"3"`, c.body); rec.Code != http.StatusForbidden {
			t.Fatalf("viewer %s %s: %d", c.method, c.path, rec.Code)
		}
	}
	if len(fc.calls) != 0 {
		t.Fatalf("a forbidden request reached the service: %v", fc.calls)
	}
	sim := uuid.New().String()
	for _, p := range []string{"/simulations/" + sim, "/simulations/" + sim + "/input-set", "/simulations/" + sim + "/reproduction"} { // viewer has policy:read and network:read
		if rec := changeReq(srv, "GET", "/api/v1/change-requests/"+id+p, viewer, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("viewer GET %s: %d", p, rec.Code)
		}
	}
	if rec := changeReq(srv, "GET", "/api/v1/change-requests", "", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list: %d", rec.Code)
	}
}

func TestChangeRoutes_IfMatchIsRequiredAndPassedThrough(t *testing.T) {
	fc := &fakeChanges{}
	srv, token := newChangeServer(t, fc)
	op := token(domain.RoleOperator)
	id := uuid.New().String()
	for _, p := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/change-requests/" + id + "/content", `{"rules":[],"default_action":"deny"}`},
		{"POST", "/api/v1/change-requests/" + id + "/validations", ""},
		{"POST", "/api/v1/change-requests/" + id + "/cancel", ""},
		{"POST", "/api/v1/change-requests/" + id + "/simulations", ""},
	} {
		if rec := changeReq(srv, p.method, p.path, op, "", p.body); rec.Code != http.StatusPreconditionRequired {
			t.Fatalf("%s %s without If-Match: %d", p.method, p.path, rec.Code)
		}
		if rec := changeReq(srv, p.method, p.path, op, `"abc"`, p.body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s %s with a non-revision If-Match: %d", p.method, p.path, rec.Code)
		}
		fc.expected = 0
		if rec := changeReq(srv, p.method, p.path, op, `W/"7"`, p.body); rec.Code >= 300 || fc.expected != 7 {
			t.Fatalf("%s %s: %d, service saw revision %d", p.method, p.path, rec.Code, fc.expected)
		}
	}
	if strings.Join(fc.calls, ",") != "edit,validate,cancel,simulate" {
		t.Fatalf("only valid requests reach the service: %v", fc.calls)
	}
	fc.err = &change.RevisionConflict{Current: 9, State: change.StateDraft}
	rec := changeReq(srv, "PUT", "/api/v1/change-requests/"+id+"/content", op, `"7"`, `{"rules":[],"default_action":"deny"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "revision 9") {
		t.Fatalf("revision conflict: %d %s", rec.Code, rec.Body.String())
	}
}
