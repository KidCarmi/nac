package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/endpoint"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/internal/policy"
)

// ---- fakes ----

type fakeEndpoints struct {
	created *domain.Endpoint
	listErr error
}

func (f *fakeEndpoints) Create(_ context.Context, in endpoint.CreateInput) (*domain.Endpoint, error) {
	e := &domain.Endpoint{ID: uuid.New(), MAC: in.MAC, AccessStatus: domain.AccessUnknown}
	f.created = e
	return e, nil
}
func (f *fakeEndpoints) Get(context.Context, uuid.UUID) (*domain.Endpoint, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeEndpoints) Update(_ context.Context, id uuid.UUID, _ endpoint.UpdateInput) (*domain.Endpoint, error) {
	return &domain.Endpoint{ID: id}, nil
}
func (f *fakeEndpoints) Delete(context.Context, uuid.UUID) error { return nil }
func (f *fakeEndpoints) List(context.Context, domain.EndpointFilter, domain.Page) ([]domain.Endpoint, error) {
	return []domain.Endpoint{}, f.listErr
}

type fakePolicies struct{}

func (fakePolicies) Create(_ context.Context, in policy.Input) (*domain.Policy, error) {
	return &domain.Policy{ID: uuid.New(), Name: in.Name, Action: in.Action}, nil
}
func (fakePolicies) Get(context.Context, uuid.UUID) (*domain.Policy, error) {
	return nil, domain.ErrNotFound
}
func (fakePolicies) Update(_ context.Context, id uuid.UUID, _ policy.Input) (*domain.Policy, error) {
	return &domain.Policy{ID: id}, nil
}
func (fakePolicies) Delete(context.Context, uuid.UUID) error { return nil }
func (fakePolicies) List(context.Context, domain.Page) ([]domain.Policy, error) {
	return []domain.Policy{}, nil
}

type fakeAccess struct{ decision domain.AccessDecision }

func (f fakeAccess) Evaluate(context.Context, access.EvaluateInput) (domain.AccessDecision, error) {
	return f.decision, nil
}

// fakeOperators is an in-memory operator store; the auth middleware looks
// every token's operator up, so tokens must name a stored operator.
type fakeOperators struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*domain.Operator
}

func (f *fakeOperators) put(o *domain.Operator) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byID == nil {
		f.byID = map[uuid.UUID]*domain.Operator{}
	}
	f.byID[o.ID] = o
}
func (f *fakeOperators) Create(_ context.Context, o *domain.Operator) error { f.put(o); return nil }
func (f *fakeOperators) GetByID(_ context.Context, id uuid.UUID) (*domain.Operator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o, ok := f.byID[id]; ok {
		c := *o
		return &c, nil
	}
	return nil, domain.ErrNotFound
}
func (f *fakeOperators) GetByEmail(_ context.Context, email string) (*domain.Operator, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.byID {
		if o.Email == email {
			c := *o
			return &c, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeOperators) List(context.Context, domain.Page) ([]domain.Operator, error) {
	return nil, nil
}
func (f *fakeOperators) SetPassword(_ context.Context, id uuid.UUID, hash string, at time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.byID[id]
	if !ok {
		return 0, domain.ErrNotFound
	}
	o.PasswordHash, o.TokenVersion, o.PasswordChangedAt = hash, o.TokenVersion+1, &at
	return o.TokenVersion, nil
}

type fakeAudit struct{ events []domain.AuditEvent }

func (f *fakeAudit) Append(_ context.Context, e *domain.AuditEvent) error {
	f.events = append(f.events, *e)
	return nil
}
func (f *fakeAudit) List(context.Context, domain.Page) ([]domain.AuditEvent, error) {
	return f.events, nil
}

// ---- harness ----

const testServiceToken = "test-service-token-abcdefghij"

type harness struct {
	srv       *Server
	tokens    *auth.TokenIssuer
	endpoints *fakeEndpoints
	auditRepo *fakeAudit
	operator  *domain.Operator
	operators *fakeOperators
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pwHash, err := auth.HashPassword("s3cret-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	op := &domain.Operator{ID: uuid.New(), Email: "admin@nac.local", PasswordHash: pwHash, Role: domain.RoleAdmin}

	tokens, err := auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), "nac", "nac-api", time.Hour)
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}

	cfg := &config.Config{
		Env:  "development",
		HTTP: config.HTTPConfig{MaxBodyBytes: 1 << 20, RateLimitPerMin: 1000},
		Auth: config.AuthConfig{ServiceToken: testServiceToken},
	}
	eps := &fakeEndpoints{}
	auditRepo := &fakeAudit{}
	ops := &fakeOperators{}
	ops.put(op)

	srv := New(Deps{
		Config:    cfg,
		Logger:    observability.NewLogger("error", "json"),
		Metrics:   observability.NewMetrics(),
		Tokens:    tokens,
		Endpoints: eps,
		Policies:  fakePolicies{},
		Access:    fakeAccess{decision: domain.AccessDecision{Endpoint: "aa:bb:cc:dd:ee:ff", Action: domain.ActionAllow, Status: domain.AccessAuthorized}},
		Operators: ops,
		AuditRepo: auditRepo,
		Audit:     audit.NewRecorder(auditRepo),
	})
	return &harness{srv: srv, tokens: tokens, endpoints: eps, auditRepo: auditRepo, operator: op, operators: ops}
}

func (h *harness) do(method, path, token string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func (h *harness) operatorToken(t *testing.T, role domain.Role) string {
	t.Helper()
	op := &domain.Operator{ID: uuid.New(), Email: "x-" + string(role) + "@nac.local", Role: role}
	h.operators.put(op)
	tok, _, err := h.tokens.Issue(op)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return tok
}

// ---- tests ----

func TestHealthz_Public(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz=%d", rec.Code)
	}
}

func TestSecurityHeadersPresent(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/healthz", "", nil)
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("header %s=%q want %q", k, got, want)
		}
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Errorf("expected correlation id header")
	}
}

func TestEndpoints_RequireAuth(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/v1/endpoints", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestEndpoints_ViewerCannotWrite(t *testing.T) {
	h := newHarness(t)
	tok := h.operatorToken(t, domain.RoleViewer)
	rec := h.do(http.MethodPost, "/api/v1/endpoints", tok, map[string]string{"mac": "aa:bb:cc:dd:ee:ff"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer write, got %d", rec.Code)
	}
}

func TestEndpoints_OperatorCanCreate(t *testing.T) {
	h := newHarness(t)
	tok := h.operatorToken(t, domain.RoleOperator)
	rec := h.do(http.MethodPost, "/api/v1/endpoints", tok, map[string]string{"mac": "aa:bb:cc:dd:ee:ff"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	if h.endpoints.created == nil {
		t.Fatalf("expected endpoint to be created")
	}
}

func TestCreateEndpoint_RejectsUnknownFields(t *testing.T) {
	h := newHarness(t)
	tok := h.operatorToken(t, domain.RoleOperator)
	rec := h.do(http.MethodPost, "/api/v1/endpoints", tok, map[string]any{"mac": "aa:bb:cc:dd:ee:ff", "evil": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", rec.Code)
	}
}

func TestLogin_Success(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": "admin@nac.local", "password": "s3cret-password",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Fatalf("expected token in response")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": "admin@nac.local", "password": "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLogin_UnknownUserSameStatusAsWrongPassword(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": "ghost@nac.local", "password": "whatever",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unknown user (no enumeration), got %d", rec.Code)
	}
}

func TestAccessEvaluate_RequiresServiceToken(t *testing.T) {
	h := newHarness(t)
	// Operator JWT must NOT be accepted on the service-token endpoint.
	tok := h.operatorToken(t, domain.RoleAdmin)
	rec := h.do(http.MethodPost, "/api/v1/access/evaluate", tok, map[string]string{"mac": "aa:bb:cc:dd:ee:ff"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with operator token, got %d", rec.Code)
	}
}

func TestAccessEvaluate_WithServiceToken(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/v1/access/evaluate", testServiceToken, map[string]string{"mac": "aa-bb-cc-dd-ee-ff"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var dec domain.AccessDecision
	if err := json.Unmarshal(rec.Body.Bytes(), &dec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.Action != domain.ActionAllow {
		t.Fatalf("expected allow decision, got %q", dec.Action)
	}
}

func TestAccessEvaluate_InvalidMAC(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodPost, "/api/v1/access/evaluate", testServiceToken, map[string]string{"mac": "nope"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for bad MAC, got %d", rec.Code)
	}
}

func TestUnknownRoute_404Problem(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/v1/nope", h.operatorToken(t, domain.RoleAdmin), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
