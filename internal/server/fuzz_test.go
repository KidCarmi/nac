package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// FuzzEvaluateBody feeds arbitrary request bodies to the access-evaluation
// endpoint (authenticated with the service token) and asserts the server never
// responds with a 5xx. A 5xx would mean either an unrecovered panic (surfaced
// as 500 by the recoverer) or an unhandled error path — both are defects.
// Well-formed-but-invalid input must be rejected with 4xx, not 5xx.
func FuzzEvaluateBody(f *testing.F) {
	seeds := []string{
		`{"mac":"aa:bb:cc:dd:ee:ff"}`,
		`{"mac":"nope"}`,
		`{`,
		``,
		`{"mac":"aa:bb:cc:dd:ee:ff","posture":"bad"}`,
		`{"mac":"aa:bb:cc:dd:ee:ff","attributes":{"k":"v"}}`,
		`{"unknown":true}`,
		`[]`,
		`null`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	h := newHarness(f)
	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/access/evaluate", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testServiceToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.srv.Handler().ServeHTTP(rec, req)
		if rec.Code >= 500 {
			t.Fatalf("5xx (%d) for body %q: %s", rec.Code, body, rec.Body.String())
		}
	})
}
