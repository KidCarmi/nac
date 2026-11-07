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
	in := domain.Inputs{}
	in.Set("device_type", "laptop", domain.SourceEndpointRecord, nil, false)
	in.Set("attr.owner", "alice", domain.SourceEndpointRecord, nil, false)
	in["posture"] = domain.Evidence{Value: "compliant", Status: domain.EvidenceStale, Source: domain.SourcePosture}

	M, N, I := domain.CondMatch, domain.CondNoMatch, domain.CondInsufficient
	cases := []struct {
		name string
		c    domain.Condition
		want domain.ConditionOutcome
	}{
		{"eq match", domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"laptop"}}, M},
		{"eq miss", domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"phone"}}, N},
		{"ne match", domain.Condition{Field: "device_type", Operator: domain.OpNotEqual, Values: []string{"phone"}}, M},
		{"ne on missing field is insufficient, not true", domain.Condition{Field: "nope", Operator: domain.OpNotEqual, Values: []string{"x"}}, I},
		{"eq on missing field is insufficient", domain.Condition{Field: "nope", Operator: domain.OpEquals, Values: []string{"x"}}, I},
		{"in match", domain.Condition{Field: "device_type", Operator: domain.OpIn, Values: []string{"phone", "laptop"}}, M},
		{"in miss", domain.Condition{Field: "device_type", Operator: domain.OpIn, Values: []string{"phone"}}, N},
		{"in on missing is insufficient", domain.Condition{Field: "nope", Operator: domain.OpIn, Values: []string{"x"}}, I},
		{"exists true", domain.Condition{Field: "attr.owner", Operator: domain.OpExists}, M},
		{"exists false on missing", domain.Condition{Field: "attr.nope", Operator: domain.OpExists}, N},
		{"absent true on missing", domain.Condition{Field: "attr.nope", Operator: domain.OpAbsent}, M},
		{"absent false on present", domain.Condition{Field: "attr.owner", Operator: domain.OpAbsent}, N},
		{"stale treated as missing for eq", domain.Condition{Field: "posture", Operator: domain.OpEquals, Values: []string{"compliant"}}, I},
		{"stale satisfies absent", domain.Condition{Field: "posture", Operator: domain.OpAbsent}, M},
		{"unknown operator fails closed", domain.Condition{Field: "device_type", Operator: "regex", Values: []string{".*"}}, N},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := conditionOutcome(tc.c, in.Get(tc.c.Field)); got != tc.want {
				t.Fatalf("outcome=%v want %v", got, tc.want)
			}
		})
	}
}

// Regression: before the evidence model, "posture ne noncompliant -> allow"
// granted access to a device with NO posture result, because a missing field
// compared as "not equal". Missing evidence must never satisfy an allow rule.
func TestEvaluate_MissingPostureDoesNotSatisfyAllowRule(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	allow := mustPolicy("allow-not-noncompliant", 10, domain.ActionAllow, "corp",
		domain.Condition{Field: "posture", Operator: domain.OpNotEqual, Values: []string{"noncompliant"}})

	for _, ep := range []domain.Endpoint{
		{MAC: "aa:bb:cc:dd:ee:ff"},                                 // no posture at all
		{MAC: "aa:bb:cc:dd:ee:ff", Posture: domain.PostureUnknown}, // explicit unknown
	} {
		d := e.Evaluate(ep, []domain.Policy{allow})
		if d.Action != domain.ActionDeny {
			t.Fatalf("posture=%q: expected default deny, got %q", ep.Posture, d.Action)
		}
		if len(d.InsufficientEvidence) != 1 || d.InsufficientEvidence[0] != "posture" {
			t.Fatalf("expected posture reported as insufficient evidence, got %v", d.InsufficientEvidence)
		}
		if d.Trace[0].Outcome != domain.RuleInsufficient {
			t.Fatalf("trace outcome = %q", d.Trace[0].Outcome)
		}
	}

	compliant := domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff", Posture: domain.PostureCompliant}
	if d := e.Evaluate(compliant, []domain.Policy{allow}); d.Action != domain.ActionAllow {
		t.Fatalf("compliant endpoint should be allowed, got %q", d.Action)
	}
}

func TestEvaluateInputs_InsufficientRuleFallsThroughAndTraceIsComplete(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	in := domain.Inputs{}
	in.Set("identity.bound", "true", domain.SourceBinding, nil, true)
	rules := []domain.Policy{
		mustPolicy("quarantine-noncompliant", 10, domain.ActionQuarantine, "quarantine",
			domain.Condition{Field: "posture", Operator: domain.OpEquals, Values: []string{"noncompliant"}}),
		mustPolicy("quarantine-no-posture", 20, domain.ActionQuarantine, "quarantine",
			domain.Condition{Field: "posture", Operator: domain.OpAbsent}),
		mustPolicy("allow-bound", 30, domain.ActionAllow, "corp",
			domain.Condition{Field: "identity.bound", Operator: domain.OpEquals, Values: []string{"true"}}),
	}
	d := e.EvaluateInputs("subject", in, rules)
	if d.MatchedRule != "quarantine-no-posture" || d.Segment != "quarantine" {
		t.Fatalf("expected explicit absent rule to quarantine, got %q/%q", d.MatchedRule, d.Segment)
	}
	want := []domain.RuleOutcome{domain.RuleInsufficient, domain.RuleMatched, domain.RuleNotReached}
	for i, w := range want {
		if d.Trace[i].Outcome != w {
			t.Fatalf("trace[%d]=%q want %q", i, d.Trace[i].Outcome, w)
		}
	}
}

func TestEvaluateInputs_DefiniteNoMatchBeatsInsufficient(t *testing.T) {
	e := NewEngine().WithClock(fixedClock())
	in := domain.Inputs{}
	in.Set("device_type", "phone", domain.SourceEndpointRecord, nil, false)
	r := mustPolicy("laptops-with-posture", 10, domain.ActionAllow, "corp",
		domain.Condition{Field: "device_type", Operator: domain.OpEquals, Values: []string{"laptop"}},
		domain.Condition{Field: "posture", Operator: domain.OpEquals, Values: []string{"compliant"}})
	d := e.EvaluateInputs("s", in, []domain.Policy{r})
	if d.Trace[0].Outcome != domain.RuleNotMatched || len(d.InsufficientEvidence) != 0 {
		t.Fatalf("a provable no-match must not be reported as insufficient evidence: %+v", d.Trace[0])
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
