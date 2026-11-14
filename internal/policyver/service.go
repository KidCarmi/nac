// Package policyver implements versioned policy changes: the editable draft
// (the legacy `policies` table), validation, a prediction of the draft's
// effect over recorded decision inputs, atomic publication and restoration.
//
// Publication semantics (explicit by design):
//   - Publishing activates a complete version atomically. It affects
//     authentications that happen AFTER publication. Existing sessions keep
//     the authorization they were given until they re-authenticate or an
//     operator re-authenticates them (POST /api/v1/sessions/{id}/reauthenticate).
//   - Restoring re-activates an earlier immutable version (it is recorded as a
//     new activation, never by rewriting history) with the same semantics.
//   - Session overrides (operator quarantine) are independent of policy
//     versions: publishing or restoring never clears them.
package policyver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/access"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policy"
)

// Store persists versions. PublishWorkspace and Activate must run their
// callback and writes inside one transaction holding an exclusive lock, so
// the workspace cannot change between digest check and snapshot.
type Store interface {
	Workspace(ctx context.Context) ([]domain.Policy, domain.PolicyAction, error)
	SetDraftDefaultAction(ctx context.Context, a domain.PolicyAction) error
	ActiveVersion(ctx context.Context) (*domain.PolicyVersion, error) // ErrNotFound when none
	GetVersion(ctx context.Context, number int) (*domain.PolicyVersion, error)
	ListVersions(ctx context.Context) ([]domain.PolicyVersion, error)
	ListActivations(ctx context.Context, limit int) ([]domain.PolicyActivation, error)
	PublishWorkspace(ctx context.Context, prepare func(ws []domain.Policy, def domain.PolicyAction, active *domain.PolicyVersion, view TxView) (*domain.PolicyVersion, *domain.PolicyActivation, error)) (*domain.PolicyVersion, error)
	Activate(ctx context.Context, number int, prepare func(target, active *domain.PolicyVersion, view TxView) (*domain.PolicyActivation, error), resetWorkspace bool) (*domain.PolicyActivation, error)
}

// TxView is everything a publish/restore callback may read, loaded by the
// store INSIDE the activation transaction. The callback must not query the
// store itself: it runs while the transaction holds a pooled connection and
// the activation lock, so a second connection could exhaust the pool and
// block publication indefinitely.
type TxView struct {
	NextNumber  int
	Segments    map[string]map[string]int
	SegmentsErr error
}

// RecordedCase is one historical decision's recorded inputs.
type RecordedCase struct {
	DecisionID      uuid.UUID           `json:"decision_id"`
	SessionID       *uuid.UUID          `json:"session_id,omitempty"`
	Subject         string              `json:"subject"`
	OccurredAt      time.Time           `json:"occurred_at"`
	Inputs          domain.Inputs       `json:"-"`
	RecordedAction  domain.PolicyAction `json:"recorded_action"`
	RecordedSegment string              `json:"recorded_segment,omitempty"`
	RecordedVersion int                 `json:"recorded_version,omitempty"`
	// Override is the session's CURRENT operator override ("quarantine" or
	// ""). It takes precedence over policy at authentication, so the preview
	// applies it too.
	Override string `json:"override,omitempty"`
}

// CaseSource supplies the latest recorded decision per session/identity.
type CaseSource interface {
	RecentCases(ctx context.Context, since time.Time, limit int) ([]RecordedCase, error)
}

// SegmentSource reports, per registered switch, the segment->VLAN mapping.
type SegmentSource interface {
	SegmentMappings(ctx context.Context) (map[string]map[string]int, error)
}

// Service is the policy-version lifecycle.
type Service struct {
	store         Store
	cases         CaseSource
	segments      SegmentSource
	defaultAction domain.PolicyAction
	previewWindow time.Duration
	now           func() time.Time
}

// New constructs a Service. cases and segments may be nil.
func New(store Store, cases CaseSource, segments SegmentSource, defaultAction domain.PolicyAction) *Service {
	if !defaultAction.Valid() {
		defaultAction = domain.ActionDeny
	}
	return &Service{store: store, cases: cases, segments: segments, defaultAction: defaultAction,
		previewWindow: 30 * 24 * time.Hour, now: time.Now}
}

// SetSources wires the case and segment sources after construction (they are
// built later in the composition root).
func (s *Service) SetSources(cases CaseSource, segments SegmentSource) {
	s.cases, s.segments = cases, segments
}

// Validation is the result of validating a complete rule set.
type Validation struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// Draft is the editable workspace plus its validation and digest.
type Draft struct {
	Rules             []domain.Policy     `json:"rules"`
	DefaultAction     domain.PolicyAction `json:"default_action"`
	Digest            string              `json:"digest"`
	Validation        Validation          `json:"validation"`
	ActiveVersion     *int                `json:"active_version,omitempty"`
	DiffersFromActive bool                `json:"differs_from_active"`
}

// EnsureInitialVersion publishes the current workspace as version 1 when no
// version exists yet (first boot or upgrade from the unversioned schema), so
// the decision path always has an active version.
func (s *Service) EnsureInitialVersion(ctx context.Context) error {
	if _, err := s.store.ActiveVersion(ctx); err == nil {
		return nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	_, err := s.store.PublishWorkspace(ctx, func(ws []domain.Policy, def domain.PolicyAction, active *domain.PolicyVersion, view TxView) (*domain.PolicyVersion, *domain.PolicyActivation, error) {
		if active != nil {
			return nil, nil, errAlreadyActive
		}
		v := s.newVersion(ws, s.resolveDefault(def), view.NextNumber, "system", "initial version created from existing policies")
		return v, &domain.PolicyActivation{Kind: domain.ActivationInitial, ActivatedBy: "system", ActivatedAt: v.CreatedAt}, nil
	})
	if errors.Is(err, errAlreadyActive) {
		return nil
	}
	return err
}

var errAlreadyActive = errors.New("a version is already active")

// ActiveRuleSet returns what the decision path evaluates.
func (s *Service) ActiveRuleSet(ctx context.Context) (domain.RuleSet, error) {
	v, err := s.store.ActiveVersion(ctx)
	if err != nil {
		return domain.RuleSet{}, fmt.Errorf("load active policy version: %w", err)
	}
	return domain.RuleSet{VersionID: v.ID, VersionNumber: v.Number, Rules: v.Rules, DefaultAction: v.DefaultAction}, nil
}

// Draft returns the workspace with validation.
func (s *Service) Draft(ctx context.Context) (*Draft, error) {
	ws, def, err := s.store.Workspace(ctx)
	if err != nil {
		return nil, err
	}
	def = s.resolveDefault(def)
	d := &Draft{Rules: canonicalRules(ws), DefaultAction: def, Digest: Digest(ws, def)}
	d.Validation = s.Validate(ctx, ws, def)
	if active, err := s.store.ActiveVersion(ctx); err == nil {
		n := active.Number
		d.ActiveVersion = &n
		d.DiffersFromActive = active.Digest != d.Digest
	}
	return d, nil
}

// SetDraftDefaultAction changes the default action the next publish uses.
func (s *Service) SetDraftDefaultAction(ctx context.Context, a domain.PolicyAction) error {
	if !a.Valid() {
		return fmt.Errorf("%w: default_action must be allow, deny or quarantine", domain.ErrInvalidInput)
	}
	return s.store.SetDraftDefaultAction(ctx, a)
}

// Validate checks a complete rule set. Errors block publication; warnings
// are shown to the operator.
func (s *Service) Validate(ctx context.Context, rules []domain.Policy, def domain.PolicyAction) Validation {
	if s.segments == nil {
		return s.validate(rules, def, nil, nil, false)
	}
	maps, err := s.segments.SegmentMappings(ctx)
	return s.validate(rules, def, maps, err, true)
}

// Active returns the active (immutable) policy version.
func (s *Service) Active(ctx context.Context) (*domain.PolicyVersion, error) {
	return s.store.ActiveVersion(ctx)
}

// SegmentEnvironment is the switch segment-to-VLAN environment validation
// checks against. Checked is false when no segment source is configured.
type SegmentEnvironment struct {
	Checked bool                      `json:"checked"`
	Maps    map[string]map[string]int `json:"maps"`
	Error   string                    `json:"error,omitempty"`
}

// Environment reads the current segment environment once, so a caller can
// both validate against it and record exactly what it validated against.
func (s *Service) Environment(ctx context.Context) SegmentEnvironment {
	if s.segments == nil {
		return SegmentEnvironment{Maps: map[string]map[string]int{}}
	}
	maps, err := s.segments.SegmentMappings(ctx)
	env := SegmentEnvironment{Checked: true, Maps: maps}
	if env.Maps == nil {
		env.Maps = map[string]map[string]int{}
	}
	if err != nil {
		env.Error = err.Error()
	}
	return env
}

// ValidateAgainst validates rules against a previously read environment.
func (s *Service) ValidateAgainst(rules []domain.Policy, def domain.PolicyAction, env SegmentEnvironment) Validation {
	var err error
	if env.Error != "" {
		err = errors.New(env.Error)
	}
	return s.validate(rules, def, env.Maps, err, env.Checked)
}

// validateTx validates with VLAN mappings the store read inside the
// activation transaction (no extra connection).
func (s *Service) validateTx(rules []domain.Policy, def domain.PolicyAction, view TxView) Validation {
	return s.validate(rules, def, view.Segments, view.SegmentsErr, s.segments != nil)
}

func (s *Service) validate(rules []domain.Policy, def domain.PolicyAction, maps map[string]map[string]int, mapsErr error, checkSegments bool) Validation {
	v := Validation{Errors: []string{}, Warnings: []string{}}
	names := map[string]bool{}
	sorted := canonicalRules(rules)
	catchAllSeen := ""
	enabled := 0
	needSegments := map[string][]string{} // segment -> rules

	for _, r := range sorted {
		label := fmt.Sprintf("rule %q", r.Name)
		if err := policy.ValidateRule(policy.Input{
			Name: r.Name, Description: r.Description, Priority: r.Priority, Enabled: r.Enabled,
			Conditions: r.Conditions, Action: r.Action, Segment: r.Segment,
		}); err != nil {
			v.Errors = append(v.Errors, fmt.Sprintf("%s: %s", label, strings.TrimPrefix(err.Error(), domain.ErrInvalidInput.Error()+": ")))
		}
		key := strings.ToLower(r.Name)
		if names[key] {
			v.Errors = append(v.Errors, fmt.Sprintf("%s: duplicate name", label))
		}
		names[key] = true
		if !r.Enabled {
			continue
		}
		enabled++
		if catchAllSeen != "" {
			v.Warnings = append(v.Warnings, fmt.Sprintf("%s is unreachable: catch-all rule %q (no conditions) has higher priority", label, catchAllSeen))
		}
		if len(r.Conditions) == 0 && catchAllSeen == "" {
			catchAllSeen = r.Name
		}
		switch r.Action {
		case domain.ActionAllow, domain.ActionQuarantine:
			if r.Segment == "" {
				v.Errors = append(v.Errors, fmt.Sprintf("%s: %s requires a segment (e.g. corp or quarantine) that maps to a VLAN", label, r.Action))
			} else {
				needSegments[r.Segment] = append(needSegments[r.Segment], r.Name)
			}
		}
	}
	if def == domain.ActionAllow || def == domain.ActionQuarantine {
		v.Errors = append(v.Errors, fmt.Sprintf("default action %q is not supported on the RADIUS path without a segment; use deny or an explicit catch-all rule", def))
	}
	if enabled == 0 {
		v.Warnings = append(v.Warnings, "no enabled rules: every device receives the default action ("+string(def)+")")
	}

	if checkSegments {
		switch {
		case mapsErr != nil:
			v.Errors = append(v.Errors, "could not load switch VLAN mappings: "+mapsErr.Error())
		case len(maps) == 0:
			v.Warnings = append(v.Warnings, "no switches registered: segment-to-VLAN mappings cannot be checked yet")
		default:
			for _, seg := range sortedStrings(needSegments) {
				for _, sw := range sortedStrings(maps) {
					if _, ok := maps[sw][seg]; !ok {
						v.Errors = append(v.Errors, fmt.Sprintf("segment %q (used by %s) has no VLAN on switch %q", seg, strings.Join(needSegments[seg], ", "), sw))
					}
				}
			}
		}
	}
	v.Valid = len(v.Errors) == 0
	return v
}

// PreviewRow is one recorded case evaluated under active and draft.
type PreviewRow struct {
	Case          RecordedCase        `json:"case"`
	ActiveAction  domain.PolicyAction `json:"active_action"`
	ActiveSegment string              `json:"active_segment,omitempty"`
	ActiveRule    string              `json:"active_rule,omitempty"`
	DraftAction   domain.PolicyAction `json:"draft_action"`
	DraftSegment  string              `json:"draft_segment,omitempty"`
	DraftRule     string              `json:"draft_rule,omitempty"`
	// Changed compares the EFFECTIVE authorization (operator override
	// applied, as at authentication). PolicyChanged compares the policy
	// results alone, so an operator still sees what the draft would do once
	// an override is restored.
	Changed              bool     `json:"changed"`
	PolicyChanged        bool     `json:"policy_changed"`
	OverrideApplies      bool     `json:"override_applies,omitempty"`
	InsufficientEvidence []string `json:"insufficient_evidence,omitempty"`
	StaleInputs          []string `json:"stale_inputs,omitempty"`
}

// Preview is the predicted effect of publishing the draft.
type Preview struct {
	Label         string       `json:"label"`
	DraftDigest   string       `json:"draft_digest"`
	ActiveVersion int          `json:"active_version"`
	Validation    Validation   `json:"validation"`
	CasesExamined int          `json:"cases_examined"`
	Changed       int          `json:"changed"`
	Insufficient  int          `json:"insufficient_evidence"`
	Rows          []PreviewRow `json:"rows"`
	Since         time.Time    `json:"since"`
}

// PreviewLabel is shown wherever a preview is displayed.
const PreviewLabel = "Prediction based on recorded decision inputs. Inputs may have changed since they were recorded; actual decisions are made at the next authentication."

// Preview evaluates the latest recorded inputs per session under the active
// version and the draft and reports changed decisions and cases with
// insufficient evidence.
func (s *Service) Preview(ctx context.Context) (*Preview, error) {
	ws, def, err := s.store.Workspace(ctx)
	if err != nil {
		return nil, err
	}
	def = s.resolveDefault(def)
	active, err := s.store.ActiveVersion(ctx)
	if err != nil {
		return nil, err
	}
	since := s.now().Add(-s.previewWindow)
	p := &Preview{Label: PreviewLabel, DraftDigest: Digest(ws, def), ActiveVersion: active.Number,
		Validation: s.Validate(ctx, ws, def), Rows: []PreviewRow{}, Since: since}
	if s.cases == nil {
		return p, nil
	}
	cases, err := s.cases.RecentCases(ctx, since, 1000)
	if err != nil {
		return nil, err
	}
	activeEngine, draftEngine := access.NewEngine(), access.NewEngine()
	activeEngine.DefaultAction, draftEngine.DefaultAction = active.DefaultAction, def
	for _, c := range cases {
		in := withCurrentOverride(c.Inputs, c.Override)
		a := activeEngine.EvaluateInputs(c.Subject, in, active.Rules)
		d := draftEngine.EvaluateInputs(c.Subject, in, ws)
		policyChanged := a.Action != d.Action || a.Segment != d.Segment
		row := PreviewRow{Case: c,
			ActiveAction: a.Action, ActiveSegment: a.Segment, ActiveRule: a.MatchedRule,
			DraftAction: d.Action, DraftSegment: d.Segment, DraftRule: d.MatchedRule,
			Changed: policyChanged, PolicyChanged: policyChanged,
			InsufficientEvidence: d.InsufficientEvidence,
		}
		if c.Override == domain.CoAQuarantine {
			// Same precedence as radiusauth.Authorize: the override decides the
			// effective authorization under both versions.
			row.OverrideApplies = true
			row.ActiveAction, row.ActiveSegment = domain.ActionQuarantine, "quarantine"
			row.DraftAction, row.DraftSegment = domain.ActionQuarantine, "quarantine"
			row.Changed = false
		}
		for f, ev := range c.Inputs {
			if ev.Status == domain.EvidenceStale {
				row.StaleInputs = append(row.StaleInputs, f)
			}
		}
		sort.Strings(row.StaleInputs)
		p.CasesExamined++
		if row.Changed {
			p.Changed++
		}
		if len(row.InsufficientEvidence) > 0 {
			p.Insufficient++
		}
		if row.Changed || row.PolicyChanged || len(row.InsufficientEvidence) > 0 {
			p.Rows = append(p.Rows, row)
		}
	}
	return p, nil
}

// PublishInput carries the operator's publish request.
type PublishInput struct {
	ExpectedDigest string
	Note           string
	Actor          string
}

// Publish validates the workspace and activates it as a new version in one
// transaction. ExpectedDigest must equal the digest of the draft the operator
// previewed; a concurrent edit makes publication fail with ErrConflict.
func (s *Service) Publish(ctx context.Context, in PublishInput) (*domain.PolicyVersion, error) {
	if in.ExpectedDigest == "" {
		return nil, fmt.Errorf("%w: expected_digest is required (preview the draft first)", domain.ErrInvalidInput)
	}
	return s.store.PublishWorkspace(ctx, func(ws []domain.Policy, def domain.PolicyAction, active *domain.PolicyVersion, view TxView) (*domain.PolicyVersion, *domain.PolicyActivation, error) {
		def = s.resolveDefault(def)
		digest := Digest(ws, def)
		if digest != in.ExpectedDigest {
			return nil, nil, fmt.Errorf("%w: the draft changed after it was previewed; preview again", domain.ErrConflict)
		}
		if active != nil && active.Digest == digest {
			return nil, nil, fmt.Errorf("%w: the draft is identical to active version %d", domain.ErrConflict, active.Number)
		}
		if val := s.validateTx(ws, def, view); !val.Valid {
			return nil, nil, fmt.Errorf("%w: %s", domain.ErrInvalidInput, strings.Join(val.Errors, "; "))
		}
		var prev *uuid.UUID
		if active != nil {
			id := active.ID
			prev = &id
		}
		v := s.newVersion(ws, def, view.NextNumber, in.Actor, in.Note)
		return v, &domain.PolicyActivation{Kind: domain.ActivationPublish, PreviousVersionID: prev,
			ActivatedBy: in.Actor, ActivatedAt: v.CreatedAt, Note: in.Note}, nil
	})
}

// RestoreInput carries a restore request.
type RestoreInput struct {
	Number int
	// ResetDraft replaces the draft with the restored version's rules, so a
	// later publish does not silently re-apply discarded changes.
	ResetDraft bool
	Note       string
	Actor      string
}

// Restore re-activates an earlier version.
func (s *Service) Restore(ctx context.Context, in RestoreInput) (*domain.PolicyActivation, error) {
	return s.store.Activate(ctx, in.Number, func(target, active *domain.PolicyVersion, view TxView) (*domain.PolicyActivation, error) {
		if active != nil && active.ID == target.ID {
			return nil, fmt.Errorf("%w: version %d is already active", domain.ErrConflict, target.Number)
		}
		// Re-validate against today's switches: a restored version may name a
		// segment a switch no longer maps.
		if val := s.validateTx(target.Rules, target.DefaultAction, view); !val.Valid {
			return nil, fmt.Errorf("%w: version %d is no longer valid: %s", domain.ErrInvalidInput, target.Number, strings.Join(val.Errors, "; "))
		}
		a := &domain.PolicyActivation{Kind: domain.ActivationRestore, ActivatedBy: in.Actor,
			ActivatedAt: s.now().UTC(), Note: in.Note}
		if active != nil {
			id := active.ID
			a.PreviousVersionID = &id
		}
		return a, nil
	}, in.ResetDraft)
}

// Versions lists all versions (newest first) with the active one flagged.
func (s *Service) Versions(ctx context.Context) ([]domain.PolicyVersion, error) {
	return s.store.ListVersions(ctx)
}

// Version returns one version by number.
func (s *Service) Version(ctx context.Context, number int) (*domain.PolicyVersion, error) {
	return s.store.GetVersion(ctx, number)
}

// Activations returns the activation history (newest first).
func (s *Service) Activations(ctx context.Context, limit int) ([]domain.PolicyActivation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return s.store.ListActivations(ctx, limit)
}

// EnabledPolicies satisfies the legacy access-service policy source: the
// /access/evaluate endpoint evaluates the ACTIVE version, not the draft.
func (s *Service) EnabledPolicies(ctx context.Context) ([]domain.Policy, error) {
	rs, err := s.ActiveRuleSet(ctx)
	if err != nil {
		return nil, err
	}
	return rs.Rules, nil
}

func (s *Service) resolveDefault(def domain.PolicyAction) domain.PolicyAction {
	if def.Valid() {
		return def
	}
	return s.defaultAction
}

func (s *Service) newVersion(ws []domain.Policy, def domain.PolicyAction, number int, actor, note string) *domain.PolicyVersion {
	return &domain.PolicyVersion{ID: uuid.New(), Number: number, Rules: canonicalRules(ws), DefaultAction: def,
		Digest: Digest(ws, def), Note: note, CreatedBy: actor, CreatedAt: s.now().UTC()}
}

// canonicalRules sorts by evaluation order and strips timestamps, which are
// workspace bookkeeping rather than policy content.
func canonicalRules(rules []domain.Policy) []domain.Policy {
	out := make([]domain.Policy, len(rules))
	copy(out, rules)
	for i := range out {
		out[i].CreatedAt, out[i].UpdatedAt = time.Time{}, time.Time{}
		if out[i].Conditions == nil {
			out[i].Conditions = []domain.Condition{}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Digest is the canonical content hash of a rule set.
func Digest(rules []domain.Policy, def domain.PolicyAction) string {
	b, _ := json.Marshal(struct {
		Rules         []domain.Policy     `json:"rules"`
		DefaultAction domain.PolicyAction `json:"default_action"`
	}{canonicalRules(rules), def})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func sortedStrings[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// withCurrentOverride returns a copy of recorded inputs whose
// session.override evidence is the session's CURRENT override, built the way
// radiusauth.buildInputs does. The recorded value may be stale: an operator
// may have restored (or quarantined) the session after that decision, for
// example when the CoA failed.
func withCurrentOverride(recorded domain.Inputs, override string) domain.Inputs {
	in := make(domain.Inputs, len(recorded)+1)
	for k, v := range recorded {
		in[k] = v
	}
	if override != "" {
		in.Set("session.override", override, domain.SourceOperator, nil, true)
	} else {
		in["session.override"] = domain.Evidence{Status: domain.EvidenceMissing, Source: domain.SourceOperator, Note: "no operator override"}
	}
	return in
}
