package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/config"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/observability"
	"github.com/kidcarmi/nac/internal/radiusauth"
)

type fakeRadius struct{ acctErr error }

func (fakeRadius) Authorize(context.Context, radiusauth.Request) (radiusauth.Reply, error) {
	return radiusauth.Reply{}, nil
}
func (fakeRadius) Reject(context.Context, radiusauth.Request) error { return nil }
func (f fakeRadius) Accounting(context.Context, radiusauth.Request) error {
	return f.acctErr
}

func radiusServer(acctErr error) *Server {
	cfg := &config.Config{Env: "development", HTTP: config.HTTPConfig{MaxBodyBytes: 1 << 20, RateLimitPerMin: 1000},
		Auth: config.AuthConfig{ServiceToken: testServiceToken}}
	repo := &fakeAudit{}
	return New(Deps{Config: cfg, Logger: observability.NewLogger("error", "json"), Metrics: observability.NewMetrics(),
		Endpoints: &fakeEndpoints{}, Policies: fakePolicies{}, Access: fakeAccess{}, Operators: fakeOperators{},
		AuditRepo: repo, Audit: audit.NewRecorder(repo), Radius: fakeRadius{acctErr: acctErr}})
}

// Review finding: when accounting cannot be persisted, nacd must answer with
// an error so FreeRADIUS records "accounting-not-forwarded" in its outage log.
func TestRadiusAccounting_PersistenceFailureIsNotReportedAsSuccess(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"persisted":           {nil, http.StatusNoContent},
		"database failure":    {errors.New("connection reset"), http.StatusInternalServerError},
		"unregistered client": {domain.ErrNotFound, http.StatusNoContent},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/radius/accounting?client=127.0.0.1",
				bytes.NewBufferString(`{"Acct-Status-Type":{"type":"integer","value":["Stop"]}}`))
			req.SetBasicAuth("freeradius", testServiceToken)
			rec := httptest.NewRecorder()
			radiusServer(tc.err).Handler().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
