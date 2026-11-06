package pilot

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kidcarmi/nac/internal/domain"
)

// legacyEAPI mimics stock vEOS-lab 4.29.2F eAPI as captured by ASTRA on
// pilot-rc2: TLS 1.2 with only an RSA key-exchange suite (AES256-SHA).
// Go 1.22+ clients do not offer RSA key exchange by default.
func legacyEAPI(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"nac","result":[{"modelName":"vEOS-lab","version":"4.29.2F"}]}`))
	}))
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS12, CipherSuites: []uint16{tls.TLS_RSA_WITH_AES_256_CBC_SHA}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestEAPI_LegacyRSAKeyExchangeSwitch(t *testing.T) {
	srv := legacyEAPI(t)
	sw := &domain.Switch{Name: "veos", EAPIURL: srv.URL, EAPIUsername: "nac-observer", EAPIPassword: "x"}

	// Default (strict) client: fails, and the error names the cause and both fixes.
	_, err := newEAPIClient(EAPITLS{InsecureSkipVerify: true}).run(context.Background(), sw, "show version")
	if err == nil {
		t.Fatal("default eAPI client negotiated an RSA key-exchange suite")
	}
	for _, want := range []string{"RSA key exchange", "ssl profile", "NAC_EAPI_TLS_ALLOW_RSA_KEX"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("handshake error does not mention %q: %v", want, err)
		}
	}

	// Opt-in, scoped to the eAPI client only: succeeds.
	res, err := newEAPIClient(EAPITLS{InsecureSkipVerify: true, AllowRSAKeyExchange: true}).run(context.Background(), sw, "show version")
	if err != nil || len(res) != 1 {
		t.Fatalf("opt-in client failed: %v", err)
	}
}

func TestEAPI_OptInDoesNotWeakenModernSwitches(t *testing.T) {
	// A modern switch still negotiates ECDHE (forward secrecy) with the opt-in set.
	var got uint16
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.TLS.CipherSuite
		_, _ = w.Write([]byte(`{"result":[{}]}`))
	}))
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	sw := &domain.Switch{EAPIURL: srv.URL}
	if _, err := newEAPIClient(EAPITLS{InsecureSkipVerify: true, AllowRSAKeyExchange: true}).run(context.Background(), sw, "show version"); err != nil {
		t.Fatal(err)
	}
	if name := tls.CipherSuiteName(got); !strings.HasPrefix(name, "TLS_ECDHE_") {
		t.Errorf("negotiated %s with a modern server; expected an ECDHE suite", name)
	}
}
