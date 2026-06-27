# 0003. Default-deny, first-match-wins policy semantics

- **Status:** Accepted
- **Date:** 2026-06-27
- **Deciders:** Platform engineering, security

## Context

A NAC engine must combine multiple, possibly conflicting rules into a single
decision. The semantics must be predictable for operators, deterministic for
auditing, and secure by default.

## Decision

- **First-match-wins** by ascending `priority` (lower = higher precedence),
  ties broken by name for determinism.
- A policy matches when **all** its conditions match (logical AND); a policy
  with no conditions is a documented catch-all.
- **Default-deny:** if no policy matches, the configured default action applies,
  and that default is `deny`. Unknown condition operators **fail closed**.
- The engine is **pure and defensively copies/sorts** its inputs so a decision
  never depends on caller-supplied ordering.

## Consequences

### Positive
- Least-privilege by construction: absence of an explicit allow means no access.
- Decisions are explainable (matched policy id + reason) and reproducible.
- Robust against malformed data: bad operators never accidentally allow.

### Negative / trade-offs
- Operators must author an explicit allow path; a misconfigured system denies
  rather than over-permits. This is the safe failure direction.
- First-match-wins (vs. evaluating all and combining) means rule order matters;
  mitigated by explicit numeric priorities and validation.

### Alternatives considered
- **Default-allow:** rejected outright on security grounds.
- **Most-specific-match / weighted combination:** more expressive but harder to
  reason about and audit; rejected for the foundation. Can be revisited via a
  new ADR if real policies demand it.
