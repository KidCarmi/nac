package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/domain"
)

func (h *harness) login(t *testing.T, email, password string) (int, string) {
	t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"email": email, "password": password})
	var out loginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out.Token
}

func TestPasswordChange_RevokesEarlierSessionsAndRotatesCredential(t *testing.T) {
	h := newHarness(t)
	code, oldTok := h.login(t, "admin@nac.local", "s3cret-password")
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	_, otherTok := h.login(t, "admin@nac.local", "s3cret-password") // e.g. another browser

	rec := h.do(http.MethodPost, "/api/v1/auth/password", oldTok,
		map[string]string{"current_password": "s3cret-password", "new_password": "a-much-better-passphrase"})
	if rec.Code != http.StatusOK {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
	var out loginResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)

	for name, tok := range map[string]string{"changing session": oldTok, "other session": otherTok} {
		if c := h.do(http.MethodGet, "/api/v1/auth/me", tok, nil).Code; c != http.StatusUnauthorized {
			t.Errorf("%s token still accepted after password change: %d", name, c)
		}
	}
	if c := h.do(http.MethodGet, "/api/v1/auth/me", out.Token, nil).Code; c != http.StatusOK {
		t.Errorf("token returned by the change is rejected: %d", c)
	}
	if c, _ := h.login(t, "admin@nac.local", "s3cret-password"); c != http.StatusUnauthorized {
		t.Errorf("old password still logs in: %d", c)
	}
	if c, _ := h.login(t, "admin@nac.local", "a-much-better-passphrase"); c != http.StatusOK {
		t.Errorf("new password does not log in: %d", c)
	}
	var ok bool
	for _, e := range h.auditRepo.events {
		ok = ok || (e.Action == "operator.password_change" && e.Outcome == "success")
	}
	if !ok {
		t.Error("password change not audited")
	}
}

func TestPasswordChange_Rejections(t *testing.T) {
	h := newHarness(t)
	_, tok := h.login(t, "admin@nac.local", "s3cret-password")
	cases := []struct {
		name, current, next string
		want                int
	}{
		{"wrong current password", "not-the-password", "a-much-better-passphrase", http.StatusForbidden},
		{"too short", "s3cret-password", "short", http.StatusUnprocessableEntity},
		{"same as current", "s3cret-password", "s3cret-password", http.StatusUnprocessableEntity},
		{"email as password", "s3cret-password", "admin@nac.local", http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		rec := h.do(http.MethodPost, "/api/v1/auth/password", tok, map[string]string{"current_password": c.current, "new_password": c.next})
		if rec.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.name, rec.Code, c.want, rec.Body)
		}
	}
	// None of the rejected attempts changed anything: the session still works.
	if c := h.do(http.MethodGet, "/api/v1/auth/me", tok, nil).Code; c != http.StatusOK {
		t.Errorf("session revoked by a rejected change: %d", c)
	}
	if c := h.do(http.MethodPost, "/api/v1/auth/password", "", map[string]string{}).Code; c != http.StatusUnauthorized {
		t.Errorf("unauthenticated change: %d", c)
	}
}

func TestOperatorRecordIsAuthoritative(t *testing.T) {
	h := newHarness(t)
	tok := h.operatorToken(t, domain.RoleOperator)
	p, err := h.tokens.Parse(tok)
	if err != nil {
		t.Fatal(err)
	}
	// Demote: the token still says "operator", the record says "viewer".
	op, _ := h.operators.GetByID(t.Context(), p.ID)
	op.Role = domain.RoleViewer
	h.operators.put(op)
	if c := h.do(http.MethodPost, "/api/v1/endpoints", tok, map[string]any{"mac": "aa:bb:cc:dd:ee:01"}).Code; c != http.StatusForbidden {
		t.Errorf("demoted operator kept write access: %d", c)
	}
	var me meResponse
	_ = json.Unmarshal(h.do(http.MethodGet, "/api/v1/auth/me", tok, nil).Body.Bytes(), &me)
	if me.Role != domain.RoleViewer || len(me.Permissions) != len(auth.PermissionsOf(domain.RoleViewer)) {
		t.Errorf("me reports %v %v", me.Role, me.Permissions)
	}
	op.Disabled = true
	h.operators.put(op)
	if c := h.do(http.MethodGet, "/api/v1/auth/me", tok, nil).Code; c != http.StatusUnauthorized {
		t.Errorf("disabled operator still authenticated: %d", c)
	}
}
