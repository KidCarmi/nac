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
	"time"

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

// Evaluate returns the access decision for ep given policies. The policies
// slice is not mutated.
func (e *Engine) Evaluate(ep domain.Endpoint, policies []domain.Policy) domain.AccessDecision {
	now := time.Now
	if e.now != nil {
		now = e.now
	}

	// Defensive copy + stable sort so callers cannot make the decision
	// depend on input ordering. Ties on priority are broken by name to keep
	// the result deterministic.
	sorted := make([]domain.Policy, len(policies))
	copy(sorted, policies)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].Name < sorted[j].Name
	})

	ctx := buildContext(ep)

	for i := range sorted {
		p := sorted[i]
		if !p.Enabled {
			continue
		}
		if policyMatches(p, ctx) {
			id := p.ID
			return domain.AccessDecision{
				Endpoint:        ep.MAC,
				Status:          statusForAction(p.Action),
				Action:          p.Action,
				Segment:         p.Segment,
				MatchedPolicyID: &id,
				Reason:          "matched policy " + p.Name,
				EvaluatedAt:     now().UTC(),
			}
		}
	}

	return domain.AccessDecision{
		Endpoint:    ep.MAC,
		Status:      statusForAction(e.DefaultAction),
		Action:      e.defaultAction(),
		Segment:     e.DefaultSegment,
		Reason:      "no policy matched; applied default action",
		EvaluatedAt: now().UTC(),
	}
}

func (e *Engine) defaultAction() domain.PolicyAction {
	if e.DefaultAction.Valid() {
		return e.DefaultAction
	}
	return domain.ActionDeny
}

// buildContext flattens an endpoint into a string keyspace that conditions
// reference by field name. Custom attributes are namespaced under
// "attr.<key>" to avoid collisions with well-known fields.
func buildContext(ep domain.Endpoint) map[string]string {
	ctx := map[string]string{
		"mac":         ep.MAC,
		"ip":          ep.IP,
		"hostname":    ep.Hostname,
		"device_type": ep.DeviceType,
		"posture":     string(ep.Posture),
	}
	for k, v := range ep.Attributes {
		ctx["attr."+k] = v
	}
	return ctx
}

func policyMatches(p domain.Policy, ctx map[string]string) bool {
	// A policy with no conditions is a catch-all. This is a deliberate,
	// documented behavior used for default/fallback policies.
	for _, c := range p.Conditions {
		if !conditionMatches(c, ctx) {
			return false
		}
	}
	return true
}

func conditionMatches(c domain.Condition, ctx map[string]string) bool {
	val, present := ctx[c.Field]
	switch c.Operator {
	case domain.OpExists:
		return present && val != ""
	case domain.OpEquals:
		return present && len(c.Values) == 1 && val == c.Values[0]
	case domain.OpNotEqual:
		// Not-equal is true when the field is absent or differs from the
		// operand. This treats "missing" as "not equal", which is the least
		// surprising behavior for exclusion rules.
		if len(c.Values) != 1 {
			return false
		}
		return val != c.Values[0]
	case domain.OpIn:
		if !present {
			return false
		}
		for _, v := range c.Values {
			if val == v {
				return true
			}
		}
		return false
	default:
		// Unknown operators never match; fail closed.
		return false
	}
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
