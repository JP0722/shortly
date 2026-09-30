package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// keys is every environment variable this package reads.
//
// setEnv clears all of them on every test so that variables already exported in
// the developer's shell (from `source .env`) cannot leak in. Without this, a
// test that forgets to set a variable would quietly pass locally and fail in CI.
var keys = []string{
	"SHORTLY_ENV",
	"SHORTLY_HTTP_ADDR",
	"SHORTLY_BASE_URL",
	"SHORTLY_DATABASE_DSN",
	"SHORTLY_REDIS_ADDR",
	"SHORTLY_LOG_LEVEL",
	"SHORTLY_SHUTDOWN_TIMEOUT",
}

// setEnv sets exactly the given variables and blanks every other one.
// t.Setenv restores the previous values when the test finishes.
func setEnv(t *testing.T, env map[string]string) {
	t.Helper() // failures are reported at the caller's line, not this one
	for _, k := range keys {
		t.Setenv(k, env[k]) // a missing map key yields "", which Load treats as unset
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	setEnv(t, map[string]string{"SHORTLY_DATABASE_DSN": "dsn"})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// Table-driven assertions: one row per field, so a failure names the field.
	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"Env", cfg.Env, "dev"},
		{"HTTPAddr", cfg.HTTPAddr, ":8080"},
		{"BaseURL", cfg.BaseURL, "http://localhost:8080"},
		{"RedisAddr", cfg.RedisAddr, "localhost:6383"},
		{"LogLevel", cfg.LogLevel, slog.LevelInfo},
		{"ShutdownTimeout", cfg.ShutdownTimeout, 15 * time.Second},
	}
	for _, c := range checks {
		if c.got != c.want {
			// t.Errorf keeps going, so one run reports every wrong field.
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}
}

func TestLoadParsesTypedValues(t *testing.T) {
	setEnv(t, map[string]string{
		"SHORTLY_DATABASE_DSN":     "dsn",
		"SHORTLY_LOG_LEVEL":        "debug",
		"SHORTLY_SHUTDOWN_TIMEOUT": "90s",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, slog.LevelDebug)
	}
	if cfg.ShutdownTimeout != 90*time.Second {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, 90*time.Second)
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		wantErrs int // errors.Join separates messages with newlines
	}{
		{
			name:     "missing required DSN",
			env:      map[string]string{},
			wantErrs: 1,
		},
		{
			name: "unparseable duration",
			env: map[string]string{
				"SHORTLY_DATABASE_DSN":     "dsn",
				"SHORTLY_SHUTDOWN_TIMEOUT": "ten seconds",
			},
			wantErrs: 1,
		},
		{
			name: "negative duration",
			env: map[string]string{
				"SHORTLY_DATABASE_DSN":     "dsn",
				"SHORTLY_SHUTDOWN_TIMEOUT": "-5s",
			},
			wantErrs: 1,
		},
		{
			name: "unknown log level",
			env: map[string]string{
				"SHORTLY_DATABASE_DSN": "dsn",
				"SHORTLY_LOG_LEVEL":    "verbose",
			},
			wantErrs: 1,
		},
		{
			name: "relative base URL",
			env: map[string]string{
				"SHORTLY_DATABASE_DSN": "dsn",
				"SHORTLY_BASE_URL":     "/not-absolute",
			},
			wantErrs: 1,
		},
		{
			name: "unknown env",
			env: map[string]string{
				"SHORTLY_DATABASE_DSN": "dsn",
				"SHORTLY_ENV":          "staging",
			},
			wantErrs: 1,
		},
		{
			// The reason parser exists: one run, every problem.
			name: "three problems at once",
			env: map[string]string{
				"SHORTLY_SHUTDOWN_TIMEOUT": "nope", // 1: unparseable
				"SHORTLY_ENV":              "qa",   // 2: not dev|prod
				// 3: SHORTLY_DATABASE_DSN missing
			},
			wantErrs: 3,
		},
	}

	for _, tt := range tests {
		// t.Run makes each case a named subtest, so `go test -run` can target one.
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.env)

			_, err := Load()
			if err == nil {
				t.Fatal("Load() error = nil, want an error")
			}

			got := strings.Count(err.Error(), "\n") + 1
			if got != tt.wantErrs {
				t.Errorf("got %d errors, want %d:\n%v", got, tt.wantErrs, err)
			}
		})
	}
}
