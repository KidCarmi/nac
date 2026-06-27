package access

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return func() time.Time { return t }
}

func mustPolicy(name string, prio int, action domain.PolicyAction, seg string, conds ...domain.Condition) domain.Policy {
	return domain.Policy{
		ID:         uuid.New(),
		Name:       name,
		Priority:   prio,
		Enabled:    true,
		Action:     action,
		Segment:    seg,
		Conditions: conds,
	}
}

func TestEvaluate_DefaultDenyWhenNoPolicies(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	d := e.Evaluate(domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff"}, nil)

	if d.Action != domain.ActionDeny {
		t.Fatalf("expected default deny, got %q", d.Action)
	}
	if d.Status != domain.AccessDenied {
		t.Fatalf("expected denied status, got %q", d.Status)
	}
	if d.MatchedPolicyID != nil {
		t.Fatalf("expected no matched policy")
	}
	if d.EvaluatedAt.IsZero() {
		t.Fatalf("expected evaluated timestamp")
	}
}

func TestEvaluate_FirstMatchWinsByPriority(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	ep := domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "printer"}

	// Lower priority number must win even when supplied out of order.
	deny := mustPolicy("deny-all", 100, domain.ActionDeny, "",
		domain.Condition{Field: "device_type", Operator: domain.OpExists})
	allow := mustPolicy("allow-printers", 10, domain.ActionAllow, "printers",
		domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"printer"}})

	d := e.Evaluate(ep, []domain.Policy{deny, allow})
	if d.Action != domain.ActionAllow {
		t.Fatalf("expected allow from higher-precedence policy, got %q", d.Action)
	}
	if d.Segment != "printers" {
		t.Fatalf("expected segment printers, got %q", d.Segment)
	}
	if d.MatchedPolicyID == nil || *d.MatchedPolicyID != allow.ID {
		t.Fatalf("expected matched policy to be allow-printers")
	}
}

func TestEvaluate_DisabledPoliciesIgnored(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	ep := domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "printer"}

	allow := mustPolicy("allow-printers", 10, domain.ActionAllow, "printers",
		domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"printer"}})
	allow.Enabled = false

	d := e.Evaluate(ep, []domain.Policy{allow})
	if d.Action != domain.ActionDeny {
		t.Fatalf("disabled policy must be skipped, got %q", d.Action)
	}
}

func TestEvaluate_AllConditionsMustMatch(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	p := mustPolicy("strict", 10, domain.ActionAllow, "corp",
		domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"laptop"}},
		domain.Condition{Field: "posture", Operator: domain.OpEquals, Values: []string{"compliant"}},
	)

	compliant := domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "laptop", Posture: domain.PostureCompliant}
	if d := e.Evaluate(compliant, []domain.Policy{p}); d.Action != domain.ActionAllow {
		t.Fatalf("expected allow for fully compliant endpoint, got %q", d.Action)
	}

	atRisk := domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "laptop", Posture: domain.PostureAtRisk}
	if d := e.Evaluate(atRisk, []domain.Policy{p}); d.Action != domain.ActionDeny {
		t.Fatalf("expected default deny when one condition fails, got %q", d.Action)
	}
}

func TestConditionOperators(t *testing.T) {
	ctx := map[string]string{
		"device_type": "laptop",
		"attr.owner":  "alice",
	}
	cases := []struct {
		name string
		c    domain.Condition
		want bool
	}{
		{"eq match", domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"laptop"}}, true},
		{"eq miss", domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"phone"}}, false},
		{"ne match", domain.Condition{Field: "device_type", Operator: domain.OpNotEqual, Values: []string{"phone"}}, true},
		{"ne on missing field is true", domain.Condition{Field: "nope", Operator: domain.OpNotEqual, Values: []string{"x"}}, true},
		{"in match", domain.Condition{Field: "device_type", Operator: domain.OpIn, Values: []string{"phone", "laptop"}}, true},
		{"in miss", domain.Condition{Field: "device_type", Operator: domain.OpIn, Values: []string{"phone"}}, false},
		{"exists true", domain.Condition{Field: "attr.owner", Operator: domain.OpExists}, true},
		{"exists false on missing", domain.Condition{Field: "attr.nope", Operator: domain.OpExists}, false},
		{"unknown operator fails closed", domain.Condition{Field: "device_type", Operator: "regex", Values: []string{".*"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := conditionMatches(tc.c, ctx); got != tc.want {
				t.Fatalf("conditionMatches=%v want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluate_CatchAllPolicyWithNoConditions(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	catchAll := mustPolicy("quarantine-unknown", 1000, domain.ActionQuarantine, "remediation")
	d := e.Evaluate(domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff"}, []domain.Policy{catchAll})
	if d.Action != domain.ActionQuarantine || d.Status != domain.AccessQuarantined {
		t.Fatalf("catch-all policy should quarantine, got action=%q status=%q", d.Action, d.Status)
	}
}

func TestEvaluate_DoesNotMutateInput(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	p1 := mustPolicy("b", 50, domain.ActionDeny, "")
	p2 := mustPolicy("a", 10, domain.ActionAllow, "ok",
		domain.Condition{Field: "mac", Operator: domain.OpExists})
	in := []domain.Policy{p1, p2}
	_ = e.Evaluate(domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff"}, in)
	if in[0].Name != "b" || in[1].Name != "a" {
		t.Fatalf("input slice order was mutated: %v", []string{in[0].Name, in[1].Name})
	}
}

func TestEvaluate_TieBreakByName(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	ep := domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "laptop"}
	pa := mustPolicy("aaa-allow", 10, domain.ActionAllow, "corp",
		domain.Condition{Field: "device_type", Operator: domain.OpExists})
	pz := mustPolicy("zzz-deny", 10, domain.ActionDeny, "",
		domain.Condition{Field: "device_type", Operator: domain.OpExists})
	d := e.Evaluate(ep, []domain.Policy{pz, pa})
	if d.MatchedPolicyID == nil || *d.MatchedPolicyID != pa.ID {
		t.Fatalf("expected name tie-break to select aaa-allow")
	}
}
