package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/pkg/problem"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	Role      string    `json:"role"`
}

// handleLogin authenticates an operator and issues a JWT. To avoid a user
// enumeration oracle, both "unknown email" and "wrong password" return the
// same generic 401 and perform a password verification against a dummy hash so
// response timing does not reveal whether the email exists.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)

	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid login payload")
		return
	}

	op, err := s.operators.GetByEmail(r.Context(), req.Email)
	if errors.Is(err, domain.ErrNotFound) {
		// Constant-work path: verify against a fixed dummy hash, then fail.
		_, _ = auth.VerifyPassword(req.Password, dummyHash)
		s.metrics.AuthFailures.WithLabelValues("unknown_operator").Inc()
		s.audit.Record(r.Context(), "operator.login", "operator", "", "failure", map[string]string{"reason": "unknown or wrong credentials", "email": truncate(req.Email, 254)})
		writeProblem(w, r, domain.ErrUnauthorized)
		return
	}
	if err != nil {
		writeProblem(w, r, err)
		return
	}

	ok, verr := auth.VerifyPassword(req.Password, op.PasswordHash)
	if verr != nil || !ok || op.Disabled {
		reason := "bad_credentials"
		if op.Disabled {
			reason = "disabled_operator"
		}
		s.metrics.AuthFailures.WithLabelValues(reason).Inc()
		// Same audited reason as for an unknown email: the audit log is
		// readable by viewers and must not become an enumeration oracle.
		s.audit.Record(r.Context(), "operator.login", "operator", "", "failure", map[string]string{"reason": "unknown or wrong credentials", "email": truncate(req.Email, 254)})
		writeProblem(w, r, domain.ErrUnauthorized)
		return
	}

	token, exp, err := s.tokens.Issue(op)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "operator.login", "operator", op.ID.String(), "success", nil)
	writeJSON(w, r, http.StatusOK, loginResponse{Token: token, ExpiresAt: exp, Role: string(op.Role)})
}

// dummyHash is a precomputed Argon2id hash of a random value, used to keep the
// login path constant-work when an email is not found. It is not a credential.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$YWJjZGVmZ2hpamtsbW5vcA$3Qe2Yx0p1m0n8sJYF2gq8m9Qz0xMv7uJ1wYJ8mWcR2A"

type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handlePasswordChange lets an authenticated operator change their OWN
// password. It requires the current password (a stolen session token alone
// cannot take over the account), stores the new hash, revokes every earlier
// session of the operator, and returns a fresh token for this session.
func (s *Server) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	defer drainAndClose(r.Body)
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		writeProblem(w, r, domain.ErrUnauthorized)
		return
	}
	var req passwordChangeRequest
	if err := decodeJSON(r, &req); err != nil {
		badRequest(w, r, "invalid password change payload")
		return
	}
	op, err := s.operators.GetByID(r.Context(), p.ID)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if ok, verr := auth.VerifyPassword(req.CurrentPassword, op.PasswordHash); verr != nil || !ok {
		s.metrics.AuthFailures.WithLabelValues("password_change_bad_current").Inc()
		s.audit.Record(r.Context(), "operator.password_change", "operator", op.ID.String(), "failure",
			map[string]string{"reason": "current password incorrect"})
		// 403, not 401: the session is still valid; only the proof failed.
		problem.New(http.StatusForbidden, "Current password incorrect",
			"The current password is incorrect; the password was not changed.").WithInstance(requestIDFrom(r)).Write(w)
		return
	}
	if reason := auth.ValidateNewPassword(req.NewPassword, op.Email); reason != "" {
		writeProblem(w, r, fmt.Errorf("%w: %s", domain.ErrInvalidInput, reason))
		return
	}
	if req.NewPassword == req.CurrentPassword {
		writeProblem(w, r, fmt.Errorf("%w: the new password must differ from the current one", domain.ErrInvalidInput))
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	v, err := s.operators.SetPassword(r.Context(), op.ID, hash, time.Now().UTC())
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	op.TokenVersion = v
	token, exp, err := s.tokens.Issue(op)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	s.audit.Record(r.Context(), "operator.password_change", "operator", op.ID.String(), "success",
		map[string]string{"sessions_revoked": "all earlier sessions of this operator"})
	writeJSON(w, r, http.StatusOK, loginResponse{Token: token, ExpiresAt: exp, Role: string(op.Role)})
}

type meResponse struct {
	ID                string      `json:"id"`
	Email             string      `json:"email"`
	Role              domain.Role `json:"role"`
	Permissions       []string    `json:"permissions"`
	PasswordChangedAt *time.Time  `json:"password_changed_at,omitempty"`
}

// handleMe returns the authenticated operator as the server sees it (current
// role and permissions from the operator record, not from the token).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		writeProblem(w, r, domain.ErrUnauthorized)
		return
	}
	op, err := s.operators.GetByID(r.Context(), p.ID)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, meResponse{ID: op.ID.String(), Email: op.Email, Role: op.Role,
		Permissions: auth.PermissionsOf(op.Role), PasswordChangedAt: op.PasswordChangedAt})
}

// truncate bounds attacker-supplied strings stored in the audit log.
func truncate(v string, n int) string {
	if len(v) > n {
		v = v[:n]
	}
	return strings.ToValidUTF8(v, "")
}
