// Package access implements the NAC policy-evaluation engine.
//
// The engine is intentionally pure: given an endpoint and an ordered set of
// policies it returns a deterministic AccessDecision with no I/O, no clock
// reads beyond an injected timestamp, and no mutation of its inputs. This
// makes the most security-critical logic in the system exhaustively unit
// testable and trivially auditable.
//
// Evaluation semantics:
//   - Policies are evaluated in ascending priority order (lower number = higher
//     precedence). The caller is responsible for supplying them sorted; the
//     engine defensively re-sorts to guarantee determinism.
//   - A policy matches when ALL of its conditions match (logical AND).
//   - The FIRST matching policy wins (first-match-wins).
//   - If no policy matches, the configured default action applies. The secure
//     default is deny ("default-deny"), aligning with least privilege.
package access

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// Engine evaluates endpoints against policies. It is safe for concurrent use:
// it holds no mutable state.
type Engine struct {
	// DefaultAction applies when no policy matches. Defaults to deny.
	DefaultAction domain.PolicyAction
	// DefaultSegment is assigned when the default action is allow/quarantine.
	DefaultSegment string
	// now is injectable for deterministic testing.
	now func() time.Time
}

// NewEngine constructs an Engine with secure defaults (default-deny).
func NewEngine() *Engine {
	return &Engine{
		DefaultAction: domain.ActionDeny,
		now:           time.Now,
	}
}

// WithClock overrides the time source; used in tests.
func (e *Engine) WithClock(now func() time.Time) *Engine {
	e.now = now
	return e
}

// Evaluate returns the access decision for ep given policies. It converts the
// endpoint record into evidence and delegates to EvaluateInputs, so the legacy
// /access/evaluate path and the RADIUS path share one set of semantics.
func (e *Engine) Evaluate(ep domain.Endpoint, policies []domain.Policy) domain.AccessDecision {
	return e.EvaluateInputs(ep.MAC, EndpointInputs(ep), policies)
}

// EndpointInputs flattens an endpoint record into evidence. Empty values and
// posture "unknown" are recorded as missing, never as a comparable value.
func EndpointInputs(ep domain.Endpoint) domain.Inputs {
	in := domain.Inputs{}
	updated := ep.UpdatedAt
	var at *time.Time
	if !updated.IsZero() {
		at = &updated
	}
	in.Set("mac", ep.MAC, domain.SourceRequest, nil, false)
	in.Set("ip", ep.IP, domain.SourceEndpointRecord, at, false)
	in.Set("hostname", ep.Hostname, domain.SourceEndpointRecord, at, false)
	in.Set("device_type", ep.DeviceType, domain.SourceEndpointRecord, at, false)
	if ep.Posture != "" && ep.Posture != domain.PostureUnknown {
		// Posture freshness comes only from its own observation time, never
		// from the record's UpdatedAt: editing a hostname is not a posture
		// report. Persisted rows always carry it (migration 0004 backfills
		// older ones); a record without it (e.g. built in memory by a caller)
		// is evaluated without a freshness limit.
		in.Set("posture", string(ep.Posture), domain.SourcePosture, ep.PostureObservedAt, false)
	} else {
		in["posture"] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourcePosture, Note: "no posture result recorded"}
	}
	for k, v := range ep.Attributes {
		in.Set("attr."+k, v, domain.SourceEndpointRecord, at, false)
	}
	return in
}

// EvaluateInputs evaluates explicit evidence against policies.
//
// Each condition is three-valued: match, no_match, or insufficient_evidence.
// A condition on a missing or stale input is insufficient_evidence for eq, ne
// and in; "exists" is a definite no_match and "absent" a definite match. A
// rule applies only when every condition matches; a rule with insufficient
// evidence is skipped (recorded in the trace) and evaluation continues. If no
// rule applies the default action is used.
func (e *Engine) EvaluateInputs(subject string, inputs domain.Inputs, policies []domain.Policy) domain.AccessDecision {
	now := time.Now
	if e.now != nil {
		now = e.now
	}

	// Defensive copy + stable sort so callers cannot make the decision
	// depend on input ordering. Ties on priority are broken by name.
	sorted := make([]domain.Policy, len(policies))
	copy(sorted, policies)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].Name < sorted[j].Name
	})

	trace := make([]domain.RuleTrace, 0, len(sorted))
	insufficient := map[string]bool{}
	var matched *domain.Policy

	for i := range sorted {
		p := sorted[i]
		rt := domain.RuleTrace{Rule: p.Name, Priority: p.Priority, Action: p.Action}
		if p.ID != uuid.Nil {
			rt.RuleID = p.ID.String()
		}
		switch {
		case matched != nil:
			rt.Outcome = domain.RuleNotReached
		case !p.Enabled:
			rt.Outcome = domain.RuleDisabled
		default:
			outcome, conds, missing := evaluateRule(p, inputs)
			rt.Outcome, rt.Conditions = outcome, conds
			switch outcome {
			case domain.RuleMatched:
				matched = &sorted[i]
			case domain.RuleInsufficient:
				for _, f := range missing {
					insufficient[f] = true
				}
			}
		}
		trace = append(trace, rt)
	}

	d := domain.AccessDecision{
		Endpoint:             subject,
		EvaluatedAt:          now().UTC(),
		Trace:                trace,
		InsufficientEvidence: sortedKeys(insufficient),
	}
	if matched != nil {
		id := matched.ID
		d.Action = matched.Action
		d.Status = statusForAction(matched.Action)
		d.Segment = matched.Segment
		d.MatchedPolicyID = &id
		d.MatchedRule = matched.Name
		d.Reason = "matched policy " + matched.Name
		return d
	}
	d.Action = e.defaultAction()
	d.Status = statusForAction(d.Action)
	d.Segment = e.DefaultSegment
	d.Reason = "no policy matched; applied default action"
	if len(d.InsufficientEvidence) > 0 {
		d.Reason += " (some rules could not be decided: missing or stale " + strings.Join(d.InsufficientEvidence, ", ") + ")"
	}
	return d
}

func (e *Engine) defaultAction() domain.PolicyAction {
	if e.DefaultAction.Valid() {
		return e.DefaultAction
	}
	return domain.ActionDeny
}

// evaluateRule evaluates every condition (no short-circuit, so the trace is
// complete). A definite no_match anywhere wins over insufficient evidence:
// the rule provably does not apply.
func evaluateRule(p domain.Policy, in domain.Inputs) (domain.RuleOutcome, []domain.ConditionTrace, []string) {
	traces := make([]domain.ConditionTrace, 0, len(p.Conditions))
	noMatch := false
	var missing []string
	for _, c := range p.Conditions {
		ev := in.Get(c.Field)
		outcome := conditionOutcome(c, ev)
		traces = append(traces, domain.ConditionTrace{
			Field: c.Field, Operator: c.Operator, Values: c.Values,
			Observed: ev.Value, Evidence: ev.Status, Outcome: outcome,
		})
		switch outcome {
		case domain.CondNoMatch:
			noMatch = true
		case domain.CondInsufficient:
			missing = append(missing, c.Field)
		}
	}
	switch {
	case noMatch:
		return domain.RuleNotMatched, traces, nil
	case len(missing) > 0:
		return domain.RuleInsufficient, traces, missing
	default:
		// A policy with no conditions is a catch-all (documented behavior).
		return domain.RuleMatched, traces, nil
	}
}

func conditionOutcome(c domain.Condition, ev domain.Evidence) domain.ConditionOutcome {
	usable := ev.Usable()
	switch c.Operator {
	case domain.OpExists:
		return boolOutcome(usable)
	case domain.OpAbsent:
		return boolOutcome(!usable)
	case domain.OpEquals:
		if !usable {
			return domain.CondInsufficient
		}
		return boolOutcome(len(c.Values) == 1 && ev.Value == c.Values[0])
	case domain.OpNotEqual:
		// Absent evidence is NOT "not equal": that would let a missing
		// posture satisfy "posture ne noncompliant -> allow".
		if !usable {
			return domain.CondInsufficient
		}
		return boolOutcome(len(c.Values) == 1 && ev.Value != c.Values[0])
	case domain.OpIn:
		if !usable {
			return domain.CondInsufficient
		}
		for _, v := range c.Values {
			if ev.Value == v {
				return domain.CondMatch
			}
		}
		return domain.CondNoMatch
	default:
		// Unknown operators never match; fail closed.
		return domain.CondNoMatch
	}
}

func boolOutcome(b bool) domain.ConditionOutcome {
	if b {
		return domain.CondMatch
	}
	return domain.CondNoMatch
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func statusForAction(a domain.PolicyAction) domain.AccessStatus {
	switch a {
	case domain.ActionAllow:
		return domain.AccessAuthorized
	case domain.ActionQuarantine:
		return domain.AccessQuarantined
	case domain.ActionDeny:
		return domain.AccessDenied
	default:
		return domain.AccessDenied
	}
}
