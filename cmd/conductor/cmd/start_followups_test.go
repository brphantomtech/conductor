package cmd

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// newTestValidationRunner builds a validationRunner with a no-sink audit writer
// suitable for unit tests; checks run via the real OS shell in the temp dir.
func newTestValidationRunner(cfg config.Validation) *validationRunner {
	return &validationRunner{
		cfg:       cfg,
		log:       zerolog.Nop(),
		audit:     audit.NewWriter(zerolog.Nop()),
		projectID: "test",
		turns:     map[string]int{},
	}
}

func TestValidationRunner_SkipsWhenDisabledOrNotAfterTurn(t *testing.T) {
	cases := []config.Validation{
		{Enabled: false, RunAfterTurn: true, Checks: []config.ValidationCheck{{ID: "x", Command: "exit 1", Severity: "error"}}},
		{Enabled: true, RunAfterTurn: false, Checks: []config.ValidationCheck{{ID: "x", Command: "exit 1", Severity: "error"}}},
	}
	for _, cfg := range cases {
		r := newTestValidationRunner(cfg)
		if err := r.Run(context.Background(), t.TempDir(), "coder"); err != nil {
			t.Fatalf("expected nil error when validation inactive, got %v", err)
		}
	}
}

func TestValidationRunner_PassingCheckReturnsNil(t *testing.T) {
	r := newTestValidationRunner(config.Validation{
		Enabled:        true,
		RunAfterTurn:   true,
		FailOnSeverity: "error",
		Checks:         []config.ValidationCheck{{ID: "ok", Name: "OK", Command: "exit 0", Severity: "error", TimeoutMS: 5000}},
	})
	if err := r.Run(context.Background(), t.TempDir(), "coder"); err != nil {
		t.Fatalf("expected nil error for passing check, got %v", err)
	}
}

func TestValidationRunner_FailingCheckAtThresholdFailsTurn(t *testing.T) {
	r := newTestValidationRunner(config.Validation{
		Enabled:        true,
		RunAfterTurn:   true,
		FailOnSeverity: "error",
		Checks:         []config.ValidationCheck{{ID: "bad", Name: "Bad", Command: "exit 1", Severity: "error", TimeoutMS: 5000}},
	})
	if err := r.Run(context.Background(), t.TempDir(), "coder"); err == nil {
		t.Fatal("expected an error when an error-severity check fails at threshold")
	}
}

func TestValidationRunner_FailingCheckBelowThresholdDoesNotFail(t *testing.T) {
	r := newTestValidationRunner(config.Validation{
		Enabled:        true,
		RunAfterTurn:   true,
		FailOnSeverity: "error",
		Checks:         []config.ValidationCheck{{ID: "warn", Name: "Warn", Command: "exit 1", Severity: "warning", TimeoutMS: 5000}},
	})
	if err := r.Run(context.Background(), t.TempDir(), "coder"); err != nil {
		t.Fatalf("expected nil error for below-threshold failure, got %v", err)
	}
}

func TestValidationRunner_TurnIndexIncrementsPerWorkspace(t *testing.T) {
	r := newTestValidationRunner(config.Validation{
		Enabled:      true,
		RunAfterTurn: true,
		Checks:       []config.ValidationCheck{{ID: "ok", Command: "exit 0", Severity: "error", TimeoutMS: 5000}},
	})
	ws := t.TempDir()
	for range 3 {
		if err := r.Run(context.Background(), ws, "coder"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if got := r.turns[ws]; got != 3 {
		t.Fatalf("expected turn index 3 after three runs, got %d", got)
	}
}
