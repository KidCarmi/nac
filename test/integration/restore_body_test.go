//go:build integration

package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// Restore's body is optional. An empty CHUNKED body (Content-Length unknown,
// as some HTTP clients send) must mean "defaults", not 400.
func TestRestoreAcceptsEmptyChunkedBody(t *testing.T) {
	resetDB(t, dsn(t))
	h := start(t, t.TempDir())
	defer h.stop()
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/v1/policy-versions/1/restore", io.NopCloser(strings.NewReader("")))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		t.Fatalf("empty chunked restore body rejected: %s", body)
	}
	t.Logf("status %d: %s", resp.StatusCode, body)
}
