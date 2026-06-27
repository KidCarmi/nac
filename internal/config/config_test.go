package config

import (
	"strings"
	"testing"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"NAC_ENV", "NAC_DB_DSN", "NAC_JWT_SIGNING_KEY", "NAC_SERVICE_TOKEN",
		"NAC_DEFAULT_ACTION", "NAC_LOG_FORMAT", "NAC_HTTP_RATE_LIMIT_PER_MIN",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_DevelopmentDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("NAC_DB_DSN", "postgres://localhost/nac")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected dev config to load, got %v", err)
	}
	if cfg.DefaultAction != "deny" {
		t.Fatalf("expected default-deny, got %q", cfg.DefaultAction)
	}
	if len(cfg.Auth.JWTSigningKey) < 32 {
		t.Fatalf("expected dev signing key fallback")
	}
}

func TestLoad_ProductionRequiresSecrets(t *testing.T) {
	clearEnv(t)
	t.Setenv("NAC_ENV", "production")
	t.Setenv("NAC_DB_DSN", "postgres://localhost/nac")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected production config to fail without secrets")
	}
	if !strings.Contains(err.Error(), "NAC_JWT_SIGNING_KEY") {
		t.Fatalf("expected JWT key error, got %v", err)
	}
	if !strings.Contains(err.Error(), "NAC_SERVICE_TOKEN") {
		t.Fatalf("expected service token error, got %v", err)
	}
}

func TestLoad_MissingDSNFails(t *testing.T) {
	clearEnv(t)
	if _, err := Load(); err == nil {
		t.Fatalf("expected failure when NAC_DB_DSN unset")
	}
}

func TestLoad_InvalidDefaultAction(t *testing.T) {
	clearEnv(t)
	t.Setenv("NAC_DB_DSN", "postgres://localhost/nac")
	t.Setenv("NAC_DEFAULT_ACTION", "explode")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NAC_DEFAULT_ACTION") {
		t.Fatalf("expected invalid default action error, got %v", err)
	}
}
