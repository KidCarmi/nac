package domain

import (
	"strings"
	"time"
)

// EvidenceStatus describes whether a decision input can be relied on.
type EvidenceStatus string

const (
	// EvidencePresent means the value was supplied by an allowed source and is
	// within its freshness window.
	EvidencePresent EvidenceStatus = "present"
	// EvidenceMissing means no allowed source supplied a value.
	EvidenceMissing EvidenceStatus = "missing"
	// EvidenceStale means a value exists but is older than its freshness
	// window. Stale evidence is treated exactly like missing evidence by the
	// engine; the value is retained only to explain the decision.
	EvidenceStale EvidenceStatus = "stale"
)

// Evidence sources. A source names *who asserted* a value, which determines
// how much it can be trusted. See InputCatalog for which source may supply
// which input.
const (
	// SourceEAPTLS: FreeRADIUS validated the client certificate chain against
	// the configured CA during EAP-TLS. Cryptographically authenticated.
	SourceEAPTLS = "eap-tls"
	// SourceNAS: asserted by a registered switch in a RADIUS request that
	// carried a valid shared-secret authenticator. Authenticates the switch,
	// NOT the endpoint (e.g. Calling-Station-Id can be spoofed by a device).
	SourceNAS = "nas"
	// SourceBinding: derived from an operator-created identity binding.
	SourceBinding = "nac-binding"
	// SourceEndpointRecord: operator-maintained endpoint inventory fields.
	SourceEndpointRecord = "endpoint-record"
	// SourcePosture: posture result written through the endpoint API by an
	// operator or posture integration, with its own observation time.
	SourcePosture = "posture"
	// SourceOperator: an explicit operator action (e.g. session quarantine).
	SourceOperator = "operator"
	// SourceRequest: legacy /access/evaluate request body (service token).
	SourceRequest = "evaluate-request"
)

// Evidence is one decision input together with its provenance.
type Evidence struct {
	Value         string         `json:"value,omitempty"`
	Source        string         `json:"source,omitempty"`
	ObservedAt    *time.Time     `json:"observed_at,omitempty"`
	Authenticated bool           `json:"authenticated"`
	Status        EvidenceStatus `json:"status"`
	// Note explains a missing/stale status in operator language.
	Note string `json:"note,omitempty"`
}

// Usable reports whether the engine may compare against this evidence.
func (e Evidence) Usable() bool { return e.Status == EvidencePresent }

// Inputs is the complete, named set of decision inputs for one evaluation.
type Inputs map[string]Evidence

// Get returns the evidence for field, or a synthetic "missing" entry.
func (in Inputs) Get(field string) Evidence {
	if e, ok := in[field]; ok {
		return e
	}
	return Evidence{Status: EvidenceMissing, Note: "no source supplied this input"}
}

// Set records a present value from source.
func (in Inputs) Set(field, value, source string, observedAt *time.Time, authenticated bool) {
	if value == "" {
		in[field] = Evidence{Status: EvidenceMissing, Source: source, Note: "source supplied an empty value"}
		return
	}
	in[field] = Evidence{Value: value, Source: source, ObservedAt: observedAt, Authenticated: authenticated, Status: EvidencePresent}
}

// InputSpec documents one decision input: the only sources allowed to supply
// it, and how long a value stays fresh (0 = valid for the request only).
type InputSpec struct {
	Field       string        `json:"field"`
	Sources     []string      `json:"sources"`
	Description string        `json:"description"`
	MaxAge      time.Duration `json:"-"`
	MaxAgeText  string        `json:"max_age,omitempty"`
}

// InputCatalog is the authoritative list of fields a policy condition may
// reference. Validation rejects unknown fields so a typo can never silently
// produce a rule that never (or always) matches. Fields prefixed "attr." are
// operator-defined endpoint attributes (source endpoint-record).
//
// Posture deliberately has a single source: it is never accepted from the
// RADIUS request, because the switch cannot attest to it.
var InputCatalog = []InputSpec{
	{Field: "identity.authenticated", Sources: []string{SourceEAPTLS}, Description: `"true" when EAP-TLS succeeded`},
	{Field: "identity.subject_cn", Sources: []string{SourceEAPTLS}, Description: "client certificate subject CN"},
	{Field: "identity.issuer", Sources: []string{SourceEAPTLS}, Description: "client certificate issuer DN"},
	{Field: "identity.serial", Sources: []string{SourceEAPTLS}, Description: "client certificate serial"},
	{Field: "identity.bound", Sources: []string{SourceBinding}, Description: `"true" when the certificate identity is bound to a device record, else "false"`},
	{Field: "mac", Sources: []string{SourceNAS, SourceRequest}, Description: "Calling-Station-Id (switch-asserted, not endpoint-authenticated); evaluate-request only on the legacy API"},
	{Field: "mac.matches_device", Sources: []string{SourceBinding}, Description: `"true" when the session MAC equals the bound device's MAC`},
	{Field: "nas.switch", Sources: []string{SourceNAS}, Description: "registered switch name"},
	{Field: "nas.port", Sources: []string{SourceNAS}, Description: "NAS-Port-Id (switch interface)"},
	{Field: "ip", Sources: []string{SourceEndpointRecord, SourceRequest}, Description: "endpoint IP"},
	{Field: "hostname", Sources: []string{SourceEndpointRecord}, Description: "device hostname"},
	{Field: "device_type", Sources: []string{SourceEndpointRecord}, Description: "device type"},
	{Field: "posture", Sources: []string{SourcePosture, SourceRequest}, Description: "posture result (compliant|at_risk|noncompliant); stale after NAC_POSTURE_MAX_AGE. " +
		"Source evaluate-request exists only on the legacy /access/evaluate API; the RADIUS path never accepts posture from a request"},
	{Field: "session.override", Sources: []string{SourceOperator}, Description: `operator session override ("quarantine")`},
}

// KnownField reports whether field may be referenced by a policy condition.
func KnownField(field string) bool {
	if strings.HasPrefix(field, "attr.") && len(field) > len("attr.") {
		return true
	}
	for _, s := range InputCatalog {
		if s.Field == field {
			return true
		}
	}
	return false
}

// ConditionOutcome is the three-valued result of evaluating one condition.
type ConditionOutcome string

const (
	CondMatch        ConditionOutcome = "match"
	CondNoMatch      ConditionOutcome = "no_match"
	CondInsufficient ConditionOutcome = "insufficient_evidence"
)

// RuleOutcome is the result of evaluating one rule.
type RuleOutcome string

const (
	RuleMatched      RuleOutcome = "matched"
	RuleNotMatched   RuleOutcome = "not_matched"
	RuleInsufficient RuleOutcome = "insufficient_evidence"
	RuleDisabled     RuleOutcome = "disabled"
	RuleNotReached   RuleOutcome = "not_reached"
)

// ConditionTrace explains one condition's evaluation.
type ConditionTrace struct {
	Field    string           `json:"field"`
	Operator MatchOperator    `json:"operator"`
	Values   []string         `json:"values,omitempty"`
	Observed string           `json:"observed,omitempty"`
	Evidence EvidenceStatus   `json:"evidence"`
	Outcome  ConditionOutcome `json:"outcome"`
}

// RuleTrace explains one rule's evaluation.
type RuleTrace struct {
	RuleID     string           `json:"rule_id,omitempty"`
	Rule       string           `json:"rule"`
	Priority   int              `json:"priority"`
	Action     PolicyAction     `json:"action"`
	Outcome    RuleOutcome      `json:"outcome"`
	Conditions []ConditionTrace `json:"conditions,omitempty"`
}
