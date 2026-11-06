//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/kidcarmi/nac/internal/app"
	"github.com/kidcarmi/nac/internal/config"
)

// TestAccount_PasswordChangeRestartAndHostReset covers the operator account
// lifecycle on PostgreSQL: change through the API (earlier sessions revoked),
// survive a restart that still carries the bootstrap password, and recover a
// forgotten password with the host-local reset.
func TestAccount_PasswordChangeRestartAndHostReset(t *testing.T) {
	resetDB(t, dsn(t))
	dir := t.TempDir()
	h := start(t, dir)

	var me map[string]any
	h.do("GET", "/api/v1/auth/me", nil, 200, &me, true)
	if me["email"] != adminEmail || me["role"] != "admin" || me["password_changed_at"] != nil {
		t.Fatalf("me before change: %v", me)
	}

	const newPass = "a-new-pilot-passphrase-42"
	h.do("POST", "/api/v1/auth/password", map[string]string{"current_password": "wrong-password-123", "new_password": newPass}, 403, nil, true)
	var changed struct{ Token string }
	h.do("POST", "/api/v1/auth/password", map[string]string{"current_password": adminPass, "new_password": newPass}, 200, &changed, true)
	h.do("GET", "/api/v1/auth/me", nil, 401, nil, true) // the session that changed it, old token
	h.token = changed.Token
	h.do("GET", "/api/v1/auth/me", nil, 200, &me, true)
	if me["password_changed_at"] == nil {
		t.Fatal("password_changed_at not reported")
	}
	h.do("POST", "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": adminPass}, 401, nil, false)

	// Restart with NAC_BOOTSTRAP_ADMIN_PASSWORD still set to the initial
	// password: the bootstrap must not reset the changed password.
	h.stop()
	h2 := start2(t, dir, newPass)
	h2.do("POST", "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": adminPass}, 401, nil, false)

	// Forgotten password: host-local reset revokes the API-issued session.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	const resetPass = "host-reset-passphrase-77"
	if err := app.ResetOperatorPassword(context.Background(), cfg, adminEmail, "short"); err == nil {
		t.Fatal("reset accepted a password that violates the policy")
	}
	if err := app.ResetOperatorPassword(context.Background(), cfg, "nobody@nac.local", resetPass); err == nil {
		t.Fatal("reset of an unknown operator succeeded")
	}
	if err := app.ResetOperatorPassword(context.Background(), cfg, adminEmail, resetPass); err != nil {
		t.Fatalf("reset: %v", err)
	}
	h2.do("GET", "/api/v1/auth/me", nil, 401, nil, true)
	var login struct{ Token string }
	h2.do("POST", "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": resetPass}, 200, &login, false)
	h2.token = login.Token
	var audit struct {
		Items []map[string]any `json:"items"`
	}
	h2.do("GET", "/api/v1/audit?limit=100", nil, 200, &audit, true)
	seen := map[string]bool{}
	for _, e := range audit.Items {
		seen[e["action"].(string)+"/"+e["outcome"].(string)] = true
	}
	for _, want := range []string{"operator.password_change/failure", "operator.password_change/success", "operator.password_reset/success"} {
		if !seen[want] {
			t.Errorf("audit missing %s (have %v)", want, seen)
		}
	}
	h2.stop()
}

// start2 is start() for a restart after the admin password was changed:
// it signs in with the given password instead of the bootstrap one.
func start2(t *testing.T, dir, password string) *harness {
	t.Helper()
	h := startNoLogin(t, dir)
	var login struct{ Token string }
	h.do("POST", "/api/v1/auth/login", map[string]string{"email": adminEmail, "password": password}, 200, &login, false)
	h.token = login.Token
	return h
}
