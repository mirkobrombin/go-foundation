package parser

import (
	"testing"
	"time"
)

func TestParse_BasicTransition(t *testing.T) {
	cfg, err := Parse("initial:draft; draft->paid; paid->shipped")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.InitialState != "draft" {
		t.Errorf("initial = %q, want %q", cfg.InitialState, "draft")
	}
	if len(cfg.Transitions) != 2 {
		t.Fatalf("expected 2 transitions, got %d", len(cfg.Transitions))
	}
	if !contains(cfg.Transitions["draft"], "paid") {
		t.Error("expected draft->paid")
	}
	if !contains(cfg.Transitions["paid"], "shipped") {
		t.Error("expected paid->shipped")
	}
}

func TestParse_Wildcard(t *testing.T) {
	cfg, err := Parse("initial:active; *->cancelled")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Wildcards) != 1 || cfg.Wildcards[0] != "cancelled" {
		t.Errorf("wildcards = %v, want [cancelled]", cfg.Wildcards)
	}
}

func TestParse_Timeout(t *testing.T) {
	cfg, err := Parse("initial:pending; pending->expired [500ms]")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	rule, ok := cfg.Timeouts["pending"]
	if !ok {
		t.Fatal("expected timeout for 'pending'")
	}
	if rule.ToState != "expired" {
		t.Errorf("toState = %q, want %q", rule.ToState, "expired")
	}
	if rule.Duration != 500*time.Millisecond {
		t.Errorf("duration = %v, want %v", rule.Duration, 500*time.Millisecond)
	}
}

func TestParse_TimeoutCustomDuration(t *testing.T) {
	cfg, err := Parse("a->b [2s]")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	rule, ok := cfg.Timeouts["a"]
	if !ok {
		t.Fatal("expected timeout for 'a'")
	}
	if rule.Duration != 2*time.Second {
		t.Errorf("duration = %v, want %v", rule.Duration, 2*time.Second)
	}
}

func TestParse_MultipleTransitionsFromSameState(t *testing.T) {
	cfg, err := Parse("a->b; a->c; b->d")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Transitions["a"]) != 2 {
		t.Fatalf("expected 2 transitions from 'a', got %d", len(cfg.Transitions["a"]))
	}
}

func TestParse_NoInitial(t *testing.T) {
	cfg, err := Parse("a->b")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.InitialState != "" {
		t.Errorf("expected empty initial, got %q", cfg.InitialState)
	}
}

func TestParse_InvalidDuration(t *testing.T) {
	_, err := Parse("a->b [notaduration]")
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestParse_TrimsWhitespace(t *testing.T) {
	cfg, err := Parse("  initial:  draft  ;  draft  ->  paid  ")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.InitialState != "draft" {
		t.Errorf("initial = %q, want %q", cfg.InitialState, "draft")
	}
	if !contains(cfg.Transitions["draft"], "paid") {
		t.Error("expected draft->paid")
	}
}

func TestParse_EmptyTag(t *testing.T) {
	cfg, err := Parse("")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if cfg.InitialState != "" || len(cfg.Transitions) != 0 {
		t.Error("expected empty config for empty tag")
	}
}

func TestParse_MultipleWildcards(t *testing.T) {
	cfg, err := Parse("*->cancelled; *->archived")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(cfg.Wildcards) != 2 {
		t.Fatalf("expected 2 wildcards, got %v", cfg.Wildcards)
	}
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
