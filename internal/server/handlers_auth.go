package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/kidcarmi/nac/internal/auth"
	"github.com/kidcarmi/nac/internal/domain"
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
