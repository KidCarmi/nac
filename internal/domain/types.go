package domain

import (
	"time"

	"github.com/google/uuid"
)

// AccessStatus is the authorization state assigned to an endpoint.
type AccessStatus string

const (
	// AccessUnknown is the default for newly observed endpoints.
	AccessUnknown AccessStatus = "unknown"
	// AccessAuthorized grants normal network access.
	AccessAuthorized AccessStatus = "authorized"
	// AccessQuarantined restricts the endpoint to a remediation segment.
	AccessQuarantined AccessStatus = "quarantined"
	// AccessDenied blocks the endpoint entirely.
	AccessDenied AccessStatus = "denied"
)

// Valid reports whether s is a recognized access status.
func (s AccessStatus) Valid() bool {
	switch s {
	case AccessUnknown, AccessAuthorized, AccessQuarantined, AccessDenied:
		return true
	default:
		return false
	}
}

// PostureStatus summarizes an endpoint's compliance posture, as reported by
// an external posture-assessment integration.
type PostureStatus string

const (
	PostureUnknown      PostureStatus = "unknown"
	PostureCompliant    PostureStatus = "compliant"
	PostureAtRisk       PostureStatus = "at_risk"
	PostureNoncompliant PostureStatus = "noncompliant"
)

// Valid reports whether p is a recognized posture status.
func (p PostureStatus) Valid() bool {
	switch p {
	case PostureUnknown, PostureCompliant, PostureAtRisk, PostureNoncompliant:
		return true
	default:
		return false
	}
}

// Endpoint is a device observed on the network, keyed by its MAC address.
type Endpoint struct {
	ID           uuid.UUID     `json:"id"`
	MAC          string        `json:"mac"`
	IP           string        `json:"ip,omitempty"`
	Hostname     string        `json:"hostname,omitempty"`
	DeviceType   string        `json:"device_type,omitempty"`
	AccessStatus AccessStatus  `json:"access_status"`
	Posture      PostureStatus `json:"posture"`
	// PostureSource and PostureObservedAt record who reported posture and
	// when; posture older than NAC_POSTURE_MAX_AGE is stale evidence.
	PostureSource     string            `json:"posture_source,omitempty"`
	PostureObservedAt *time.Time        `json:"posture_observed_at,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
	LastSeen          time.Time         `json:"last_seen"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// PolicyAction is the decision a matching policy yields.
type PolicyAction string

const (
	ActionAllow      PolicyAction = "allow"
	ActionDeny       PolicyAction = "deny"
	ActionQuarantine PolicyAction = "quarantine"
)

// Valid reports whether a is a recognized policy action.
func (a PolicyAction) Valid() bool {
	switch a {
	case ActionAllow, ActionDeny, ActionQuarantine:
		return true
	default:
		return false
	}
}

// MatchOperator enumerates the comparison operators a condition may use.
type MatchOperator string

const (
	OpEquals   MatchOperator = "eq"
	OpNotEqual MatchOperator = "ne"
	OpIn       MatchOperator = "in"
	OpExists   MatchOperator = "exists"
	// OpAbsent matches when the field has no usable (present and fresh)
	// evidence, so a policy can act explicitly on missing evidence.
	OpAbsent MatchOperator = "absent"
)

// Valid reports whether o is a recognized match operator.
func (o MatchOperator) Valid() bool {
	switch o {
	case OpEquals, OpNotEqual, OpIn, OpExists, OpAbsent:
		return true
	default:
		return false
	}
}

// Condition is a single predicate evaluated against an endpoint. Field names
// a well-known endpoint attribute (see EvaluationContext for the resolvable
// keys); Values holds the comparison operand(s).
type Condition struct {
	Field    string        `json:"field"`
	Operator MatchOperator `json:"operator"`
	Values   []string      `json:"values,omitempty"`
}

// Policy is an ordered access-control rule. All conditions must match (logical
// AND) for the policy to apply; policies are evaluated in ascending Priority
// order and the first match wins.
type Policy struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Priority    int          `json:"priority"`
	Enabled     bool         `json:"enabled"`
	Conditions  []Condition  `json:"conditions"`
	Action      PolicyAction `json:"action"`
	Segment     string       `json:"segment,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// AccessDecision is the result of evaluating an endpoint against the policy
// set. It is deterministic and fully explains itself via MatchedPolicyID and
// Reason so operators can audit why a device was granted or denied access.
type AccessDecision struct {
	Endpoint        string       `json:"endpoint"` // MAC
	Status          AccessStatus `json:"status"`
	Action          PolicyAction `json:"action"`
	Segment         string       `json:"segment,omitempty"`
	MatchedPolicyID *uuid.UUID   `json:"matched_policy_id,omitempty"`
	MatchedRule     string       `json:"matched_rule,omitempty"`
	// PolicyVersion identifies the immutable version that produced this
	// decision (0 when evaluated outside versioning, e.g. a draft preview).
	PolicyVersion   int        `json:"policy_version,omitempty"`
	PolicyVersionID *uuid.UUID `json:"policy_version_id,omitempty"`
	Reason          string     `json:"reason"`
	EvaluatedAt     time.Time  `json:"evaluated_at"`
	// Trace lists every rule in evaluation order with its outcome.
	Trace []RuleTrace `json:"trace,omitempty"`
	// InsufficientEvidence lists fields whose missing or stale evidence
	// prevented a higher-priority rule from being decided.
	InsufficientEvidence []string `json:"insufficient_evidence,omitempty"`
}

// Role is an operator's authorization role within the platform.
type Role string

const (
	// RoleAdmin can manage operators, policies, endpoints, and view audit.
	RoleAdmin Role = "admin"
	// RoleOperator can manage policies and endpoints but not operators.
	RoleOperator Role = "operator"
	// RoleViewer has read-only access.
	RoleViewer Role = "viewer"
)

// Valid reports whether r is a recognized role.
func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	default:
		return false
	}
}

// Operator is an administrative user of the NAC platform. PasswordHash is the
// encoded Argon2id hash and is never serialized to clients.
type Operator struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AuditEvent is an append-only record of a security-relevant action.
type AuditEvent struct {
	ID           uuid.UUID         `json:"id"`
	OccurredAt   time.Time         `json:"occurred_at"`
	ActorID      *uuid.UUID        `json:"actor_id,omitempty"`
	ActorEmail   string            `json:"actor_email,omitempty"`
	Action       string            `json:"action"`
	ResourceType string            `json:"resource_type"`
	ResourceID   string            `json:"resource_id,omitempty"`
	Outcome      string            `json:"outcome"`
	RequestID    string            `json:"request_id,omitempty"`
	RemoteAddr   string            `json:"remote_addr,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}
