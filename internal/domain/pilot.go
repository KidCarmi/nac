package domain

import (
	"time"

	"github.com/google/uuid"
)

// Switch is a registered RADIUS client (an Arista access switch).
type Switch struct {
	ID                uuid.UUID      `json:"id"`
	Name              string         `json:"name"`
	RadiusSourceIP    string         `json:"radius_source_ip"`
	NASIP             string         `json:"nas_ip"`
	RadiusSecret      string         `json:"-"` // plaintext only in memory
	SecretSet         bool           `json:"radius_secret_set"`
	CoAPort           int            `json:"coa_port"`
	SegmentVLANs      map[string]int `json:"segment_vlans"`
	AccessPorts       []string       `json:"access_ports"`
	EOSVersion        string         `json:"eos_version"`
	RadiusServerIP    string         `json:"radius_server_ip"`
	EAPIURL           string         `json:"eapi_url,omitempty"`
	EAPIUsername      string         `json:"eapi_username,omitempty"`
	EAPIPassword      string         `json:"-"`
	EAPIPasswordSet   bool           `json:"eapi_password_set"`
	LastRadiusAt      *time.Time     `json:"last_radius_at,omitempty"`
	LastProbeAt       *time.Time     `json:"last_probe_at,omitempty"`
	LastProbeOK       *bool          `json:"last_probe_ok,omitempty"`
	LastProbeDetail   string         `json:"last_probe_detail,omitempty"`
	VerifiedAt        *time.Time     `json:"verified_at,omitempty"`
	VerifiedDetail    string         `json:"verified_detail,omitempty"`
	VerifiedSessionID *uuid.UUID     `json:"verified_session_id,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// IdentityBinding maps an authenticated certificate identity to a device.
type IdentityBinding struct {
	ID         uuid.UUID `json:"id"`
	EndpointID uuid.UUID `json:"endpoint_id"`
	Issuer     string    `json:"issuer"`
	Serial     string    `json:"serial"`
	SubjectCN  string    `json:"subject_cn,omitempty"`
	Revision   int       `json:"revision"`
	Note       string    `json:"note,omitempty"`
	CreatedBy  string    `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Session states.
const (
	SessionActive   = "active"
	SessionRejected = "rejected"
	SessionEnded    = "ended"
)

// Session is one endpoint on one switch port, keyed by switch + NAS-Port-Id +
// MAC (the lab switch sends no accounting, so Acct-Session-Id is optional).
type Session struct {
	ID               uuid.UUID  `json:"id"`
	SwitchID         uuid.UUID  `json:"switch_id"`
	SwitchName       string     `json:"switch_name,omitempty"`
	NASPortID        string     `json:"nas_port_id"`
	MAC              string     `json:"mac"`
	CallingStationID string     `json:"calling_station_id,omitempty"`
	NASIPAddress     string     `json:"nas_ip_address,omitempty"`
	UserName         string     `json:"user_name,omitempty"`
	AcctSessionID    string     `json:"acct_session_id,omitempty"`
	CertIssuer       string     `json:"cert_issuer,omitempty"`
	CertSerial       string     `json:"cert_serial,omitempty"`
	CertCN           string     `json:"cert_cn,omitempty"`
	EndpointID       *uuid.UUID `json:"endpoint_id,omitempty"`
	BindingID        *uuid.UUID `json:"binding_id,omitempty"`
	State            string     `json:"state"`
	Override         string     `json:"override,omitempty"`
	OverrideBy       string     `json:"override_by,omitempty"`
	OverrideAt       *time.Time `json:"override_at,omitempty"`
	LastDecisionID   *uuid.UUID `json:"last_decision_id,omitempty"`
	RequestedAction  string     `json:"requested_action,omitempty"`
	RequestedSegment string     `json:"requested_segment,omitempty"`
	RequestedVLAN    *int       `json:"requested_vlan,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	LastAuthAt       time.Time  `json:"last_auth_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	ObservedStatus   string     `json:"observed_status,omitempty"`
	ObservedVLAN     string     `json:"observed_vlan,omitempty"`
	ObservedAt       *time.Time `json:"observed_at,omitempty"`
	ObservedSource   string     `json:"observed_source,omitempty"`
	ObservedDetail   string     `json:"observed_detail,omitempty"`
}

// DecisionRecord is a persisted authorization decision.
type DecisionRecord struct {
	ID              uuid.UUID    `json:"id"`
	OccurredAt      time.Time    `json:"occurred_at"`
	SessionID       *uuid.UUID   `json:"session_id,omitempty"`
	SwitchID        *uuid.UUID   `json:"switch_id,omitempty"`
	Subject         string       `json:"subject"`
	Inputs          Inputs       `json:"inputs"`
	PolicyVersion   int          `json:"policy_version"`
	PolicyVersionID *uuid.UUID   `json:"policy_version_id,omitempty"`
	MatchedRule     string       `json:"matched_rule,omitempty"`
	MatchedRuleID   *uuid.UUID   `json:"matched_rule_id,omitempty"`
	Trace           []RuleTrace  `json:"trace"`
	Insufficient    []string     `json:"insufficient_evidence"`
	Action          PolicyAction `json:"action"`
	Segment         string       `json:"segment,omitempty"`
	VLAN            *int         `json:"vlan,omitempty"`
	Result          string       `json:"result"` // accept | reject
	Reason          string       `json:"reason"`
}

// Timeline event stages.
const (
	StageEAPTLS      = "eap-tls"
	StageDecision    = "nac-decision"
	StageOutage      = "outage"
	StageCoA         = "coa"
	StageObservation = "observation"
	StageAccounting  = "accounting"
	StageOperator    = "operator"
)

// AuthEvent is one timeline entry.
type AuthEvent struct {
	ID         uuid.UUID         `json:"id"`
	OccurredAt time.Time         `json:"occurred_at"`
	SessionID  *uuid.UUID        `json:"session_id,omitempty"`
	SwitchID   *uuid.UUID        `json:"switch_id,omitempty"`
	ClientIP   string            `json:"client_ip,omitempty"`
	NASPortID  string            `json:"nas_port_id,omitempty"`
	MAC        string            `json:"mac,omitempty"`
	Stage      string            `json:"stage"`
	Result     string            `json:"result"`
	Reason     string            `json:"reason"`
	Source     string            `json:"source"`
	DecisionID *uuid.UUID        `json:"decision_id,omitempty"`
	CoAID      *uuid.UUID        `json:"coa_id,omitempty"`
	Detail     map[string]string `json:"detail,omitempty"`
	ImportKey  string            `json:"-"`
}

// CoA kinds and states.
const (
	CoAQuarantine     = "quarantine"
	CoARestore        = "restore"
	CoAReauthenticate = "reauthenticate"

	CoARequested = "requested"
	CoASent      = "sent"
	CoAAcked     = "acked"
	CoANak       = "nak"
	CoATimeout   = "timeout"
	CoAFailed    = "failed"
	CoAObserved  = "observed"
)

// CoATarget is the exact set of identifiers captured when an action is
// requested. Retries use this snapshot, never a fresh lookup.
type CoATarget struct {
	SwitchID         uuid.UUID `json:"switch_id"`
	SwitchName       string    `json:"switch_name"`
	Address          string    `json:"address"` // host:port
	NASIPAddress     string    `json:"nas_ip_address"`
	NASPortID        string    `json:"nas_port_id"`
	CallingStationID string    `json:"calling_station_id"`
	UserName         string    `json:"user_name,omitempty"`
	SessionID        uuid.UUID `json:"session_id"`
	SessionStartedAt time.Time `json:"session_started_at"`
}

// CoARequest is one operator CoA action and its transport/enforcement state.
type CoARequest struct {
	ID                 uuid.UUID  `json:"id"`
	SessionID          uuid.UUID  `json:"session_id"`
	SwitchID           uuid.UUID  `json:"switch_id"`
	Kind               string     `json:"kind"`
	Target             CoATarget  `json:"target"`
	RequestedSegment   string     `json:"requested_segment,omitempty"`
	RequestedVLAN      *int       `json:"requested_vlan,omitempty"`
	State              string     `json:"state"`
	Attempts           int        `json:"attempts"`
	RadiusID           *int       `json:"radius_id,omitempty"`
	ErrorCause         *int       `json:"error_cause,omitempty"`
	Detail             string     `json:"detail,omitempty"`
	CreatedBy          string     `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	SentAt             *time.Time `json:"sent_at,omitempty"`
	RespondedAt        *time.Time `json:"responded_at,omitempty"`
	ObservedAt         *time.Time `json:"observed_at,omitempty"`
	ObservedDecisionID *uuid.UUID `json:"observed_decision_id,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
}
