// Package config loads and validates runtime configuration from the
// environment, following 12-factor principles. Configuration is immutable once
// loaded. Secrets are read from the environment (or, in production, from a
// secrets manager that populates the environment) and are never logged.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kidcarmi/nac/internal/domain"
)

// Config is the fully-resolved, validated application configuration.
type Config struct {
	Env   string // "development" | "staging" | "production"
	HTTP  HTTPConfig
	DB    DBConfig
	Redis RedisConfig
	Auth  AuthConfig
	Log   LogConfig
	// Bootstrap seeds the first administrator on an empty installation.
	Bootstrap BootstrapConfig
	// Pilot configures the RADIUS/switch pilot features.
	Pilot PilotConfig
	// DefaultAction is the access-engine fallback when no policy matches.
	DefaultAction domain.PolicyAction
}

// HTTPConfig configures the API server.
type HTTPConfig struct {
	Addr string
	// HTTPSAddr, when set, adds a TLS listener (operators) next to Addr
	// (which the installer binds to loopback for FreeRADIUS).
	HTTPSAddr         string
	TLSCertFile       string
	TLSKeyFile        string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	MaxBodyBytes      int64
	RateLimitPerMin   int
}

// DBConfig configures the PostgreSQL connection pool.
type DBConfig struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
}

// RedisConfig configures the cache. Addr empty disables caching (no-op cache).
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	TTL      time.Duration
}

// AuthConfig configures session tokens and the enforcement-point service token.
type AuthConfig struct {
	JWTSigningKey []byte
	JWTIssuer     string
	JWTAudience   string
	JWTTTL        time.Duration
	// ServiceToken authenticates trusted enforcement points calling the
	// access-evaluation endpoint. It is compared in constant time.
	ServiceToken string
}

// BootstrapConfig seeds the first administrator. Both empty disables it.
type BootstrapConfig struct {
	AdminEmail    string
	AdminPassword string
}

// PilotConfig configures evidence freshness and switch integration.
type PilotConfig struct {
	// PostureMaxAge: posture older than this is stale (treated as missing).
	PostureMaxAge time.Duration
	// ObservationMaxAge: switch-observed state older than this is shown stale.
	ObservationMaxAge time.Duration
	// SecretsKey encrypts switch RADIUS secrets and eAPI passwords at rest.
	SecretsKey string
	// CoATimeout and CoARetries bound each CoA exchange.
	CoATimeout time.Duration
	CoARetries int
	// ClientsFile is where nacd renders FreeRADIUS client definitions.
	ClientsFile string
	// OutageLog is the FreeRADIUS outage log nacd imports into the timeline.
	OutageLog string
	// ReloadStatusFile is where the reload helper records what FreeRADIUS
	// loaded; default freeradius-reload.status next to ClientsFile.
	ReloadStatusFile string
	// SkipProcessCheck (NAC_FREERADIUS_PROCESS_CHECK=off) is for a FreeRADIUS
	// on another host: nacd then cannot see whether it is running.
	SkipProcessCheck bool
	// EAPIInsecureSkipVerify accepts self-signed switch eAPI certificates.
	EAPIInsecureSkipVerify bool
	// EAPIAllowRSAKeyExchange lets the eAPI client (only) offer TLS 1.2 RSA
	// key-exchange suites, which stock EOS 4.29.2F eAPI requires.
	EAPIAllowRSAKeyExchange bool
}

// LogConfig configures structured logging.
type LogConfig struct {
	Level  string // debug|info|warn|error
	Format string // json|text
}

// Load reads configuration from the environment and validates it. It returns
// an aggregated error describing every problem found, so misconfiguration is
// surfaced all at once rather than one restart at a time.
func Load() (*Config, error) {
	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	cfg := &Config{
		Env: getEnv("NAC_ENV", "development"),
		HTTP: HTTPConfig{
			Addr:              getEnv("NAC_HTTP_ADDR", ":8080"),
			HTTPSAddr:         os.Getenv("NAC_HTTPS_ADDR"),
			TLSCertFile:       os.Getenv("NAC_TLS_CERT_FILE"),
			TLSKeyFile:        os.Getenv("NAC_TLS_KEY_FILE"),
			ReadTimeout:       getDuration("NAC_HTTP_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:      getDuration("NAC_HTTP_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:       getDuration("NAC_HTTP_IDLE_TIMEOUT", 60*time.Second),
			ReadHeaderTimeout: getDuration("NAC_HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ShutdownTimeout:   getDuration("NAC_HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			MaxBodyBytes:      int64(getInt("NAC_HTTP_MAX_BODY_BYTES", 1<<20)), // 1 MiB
			RateLimitPerMin:   getInt("NAC_HTTP_RATE_LIMIT_PER_MIN", 600),
		},
		DB: DBConfig{
			DSN:             os.Getenv("NAC_DB_DSN"),
			MaxConns:        getInt32("NAC_DB_MAX_CONNS", 10),
			MinConns:        getInt32("NAC_DB_MIN_CONNS", 2),
			MaxConnLifetime: getDuration("NAC_DB_MAX_CONN_LIFETIME", time.Hour),
		},
		Redis: RedisConfig{
			Addr:     os.Getenv("NAC_REDIS_ADDR"),
			Password: os.Getenv("NAC_REDIS_PASSWORD"),
			DB:       getInt("NAC_REDIS_DB", 0),
			TTL:      getDuration("NAC_REDIS_TTL", 30*time.Second),
		},
		Auth: AuthConfig{
			JWTSigningKey: []byte(os.Getenv("NAC_JWT_SIGNING_KEY")),
			JWTIssuer:     getEnv("NAC_JWT_ISSUER", "nac"),
			JWTAudience:   getEnv("NAC_JWT_AUDIENCE", "nac-api"),
			JWTTTL:        getDuration("NAC_JWT_TTL", time.Hour),
			ServiceToken:  os.Getenv("NAC_SERVICE_TOKEN"),
		},
		Log: LogConfig{
			Level:  getEnv("NAC_LOG_LEVEL", "info"),
			Format: getEnv("NAC_LOG_FORMAT", "json"),
		},
		Bootstrap: BootstrapConfig{
			AdminEmail:    os.Getenv("NAC_BOOTSTRAP_ADMIN_EMAIL"),
			AdminPassword: os.Getenv("NAC_BOOTSTRAP_ADMIN_PASSWORD"),
		},
		DefaultAction: domain.PolicyAction(getEnv("NAC_DEFAULT_ACTION", string(domain.ActionDeny))),
		Pilot: PilotConfig{
			PostureMaxAge:           getDuration("NAC_POSTURE_MAX_AGE", 24*time.Hour),
			ObservationMaxAge:       getDuration("NAC_OBSERVATION_MAX_AGE", 5*time.Minute),
			SecretsKey:              os.Getenv("NAC_SECRETS_KEY"),
			CoATimeout:              getDuration("NAC_COA_TIMEOUT", 3*time.Second),
			CoARetries:              getInt("NAC_COA_RETRIES", 2),
			ClientsFile:             os.Getenv("NAC_FREERADIUS_CLIENTS_FILE"),
			OutageLog:               os.Getenv("NAC_FREERADIUS_OUTAGE_LOG"),
			ReloadStatusFile:        os.Getenv("NAC_FREERADIUS_RELOAD_STATUS"),
			SkipProcessCheck:        strings.EqualFold(os.Getenv("NAC_FREERADIUS_PROCESS_CHECK"), "off"),
			EAPIInsecureSkipVerify:  getEnv("NAC_EAPI_INSECURE_SKIP_VERIFY", "false") == "true",
			EAPIAllowRSAKeyExchange: getEnv("NAC_EAPI_TLS_ALLOW_RSA_KEX", "false") == "true",
		},
	}

	// Validation. Secrets are mandatory in non-development environments and
	// must be strong; this enforces secure-by-default deployments.
	if cfg.DB.DSN == "" {
		add("NAC_DB_DSN is required")
	}
	if !cfg.DefaultAction.Valid() {
		add("NAC_DEFAULT_ACTION %q is invalid", cfg.DefaultAction)
	}
	if cfg.HTTP.HTTPSAddr != "" && (cfg.HTTP.TLSCertFile == "" || cfg.HTTP.TLSKeyFile == "") {
		add("NAC_HTTPS_ADDR requires NAC_TLS_CERT_FILE and NAC_TLS_KEY_FILE")
	}
	if cfg.HTTP.RateLimitPerMin <= 0 {
		add("NAC_HTTP_RATE_LIMIT_PER_MIN must be positive")
	}

	prod := cfg.Env != "development"
	if len(cfg.Auth.JWTSigningKey) < 32 {
		if prod {
			add("NAC_JWT_SIGNING_KEY must be at least 32 bytes")
		} else if len(cfg.Auth.JWTSigningKey) == 0 {
			// Development convenience: a clearly-marked ephemeral dev key.
			cfg.Auth.JWTSigningKey = []byte("dev-insecure-signing-key-change-me-now!")
		}
	}
	if len(cfg.Pilot.SecretsKey) < 32 {
		if prod {
			add("NAC_SECRETS_KEY must be at least 32 characters (it encrypts switch RADIUS secrets at rest)")
		} else if cfg.Pilot.SecretsKey == "" {
			cfg.Pilot.SecretsKey = "dev-insecure-secrets-key-change-me-0123456789"
		}
	}
	if cfg.Pilot.CoARetries < 0 || cfg.Pilot.CoARetries > 10 {
		add("NAC_COA_RETRIES must be 0-10")
	}
	if prod && len(cfg.Auth.ServiceToken) < 24 {
		add("NAC_SERVICE_TOKEN must be at least 24 chars in non-development environments")
	}
	switch cfg.Log.Format {
	case "json", "text":
	default:
		add("NAC_LOG_FORMAT must be json or text")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return cfg, nil
}

// IsProduction reports whether the service runs outside development.
func (c *Config) IsProduction() bool { return c.Env == "production" }

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// getInt32 reads a bounded 32-bit integer setting. Values are clamped to
// [0, math.MaxInt32] so an out-of-range or hostile environment value can never
// overflow the int32 fields it feeds (e.g. connection-pool sizes).
func getInt32(key string, def int32) int32 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			if n < 0 {
				return 0
			}
			return int32(n)
		}
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// ErrNoConfig is returned by helpers when configuration is absent. Exposed for
// callers that want to distinguish "unset" from "invalid".
var ErrNoConfig = errors.New("configuration not loaded")
