package change

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/audit"
	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
)

// Label is shown with every change request (API and console).
const Label = "Review-only change request: a proposal. Creating, validating or simulating it does not change the active policy; " +
	"policy is still published only through the policy draft (publish/restore)."

// PolicyReader is everything the change service may read about policy. It
// deliberately has no publish, restore or activation method: the service
// cannot change the active policy (tested by reflection).
type PolicyReader interface {
	Active(ctx context.Context) (*domain.PolicyVersion, error)
	Version(ctx context.Context, number int) (*domain.PolicyVersion, error)
	RecordedCases(ctx context.Context, since time.Time, limit int) ([]policyver.RecordedCase, error)
	PreviewWindow() time.Duration
	Draft(ctx context.Context) (*policyver.Draft, error)
	Environment(ctx context.Context) policyver.SegmentEnvironment
	ValidateAgainst(rules []domain.Policy, def domain.PolicyAction, env policyver.SegmentEnvironment) policyver.Validation
}

// Service is the ONE application service behind the change-request API and
// console. Handlers parse and authorize; every rule lives here.
type Service struct {
	repo   Repository
	policy PolicyReader
	now    func() time.Time
}

// NewService constructs the service.
func NewService(repo Repository, policy PolicyReader) *Service {
	return &Service{repo: repo, policy: policy, now: time.Now}
}

// Limits on proposal content (the request body is also size-limited).
const (
	MaxTitle  = 200
	MaxIntent = 4000
	MaxRules  = 500
)

// CreateInput creates a change request from explicit content or from a copy
// of the current policy draft.
type CreateInput struct {
	Title     string   `json:"title"`
	Intent    string   `json:"intent"`
	Content   *Content `json:"content,omitempty"`
	FromDraft bool     `json:"from_draft,omitempty"`
}

func (s *Service) event(ctx context.Context, action string, id uuid.UUID, meta map[string]string) *domain.AuditEvent {
	return audit.NewEvent(ctx, s.now().UTC(), action, "change_request", id.String(), audit.OutcomeSuccess, meta)
}

// normalize checks proposal content structurally and returns a copy with a
// stable ID on every rule (IDs are part of the canonical digest). Semantic
// problems (duplicate names, missing segment mappings...) are NOT input
// errors: validation records them.
func normalize(c Content) (Content, error) {
	if !c.DefaultAction.Valid() {
		return Content{}, fmt.Errorf("%w: default_action must be allow, deny or quarantine", domain.ErrInvalidInput)
	}
	if len(c.Rules) > MaxRules {
		return Content{}, fmt.Errorf("%w: at most %d rules", domain.ErrInvalidInput, MaxRules)
	}
	out := Content{DefaultAction: c.DefaultAction, Rules: make([]domain.Policy, len(c.Rules))}
	for i, r := range c.Rules {
		if strings.TrimSpace(r.Name) == "" || len(r.Name) > 200 {
			return Content{}, fmt.Errorf("%w: rule %d needs a name (at most 200 characters)", domain.ErrInvalidInput, i+1)
		}
		if !r.Action.Valid() {
			return Content{}, fmt.Errorf("%w: rule %q: action must be allow, deny or quarantine", domain.ErrInvalidInput, r.Name)
		}
		if r.ID == uuid.Nil {
			r.ID = uuid.NewSHA1(uuid.NameSpaceOID, []byte("nac-change-rule:"+strings.ToLower(r.Name)))
		}
		if r.Conditions == nil {
			r.Conditions = []domain.Condition{}
		}
		r.CreatedAt, r.UpdatedAt = time.Time{}, time.Time{}
		out.Rules[i] = r
	}
	return out, nil
}

func (s *Service) newRevision(ctx context.Context, c Content, author string) (*Revision, error) {
	active, err := s.policy.Active(ctx)
	if err != nil {
		return nil, fmt.Errorf("active policy version: %w", err)
	}
	return &Revision{Content: c, ContentDigest: c.Digest(), BaseVersionID: active.ID, BaseVersionNumber: active.Number,
		BaseVersionDigest: active.Digest, Author: author, CreatedAt: s.now().UTC()}, nil
}

// Create records a new change request at revision 1, based on the active
// policy version.
func (s *Service) Create(ctx context.Context, in CreateInput, actor string) (*ChangeRequest, error) {
	title := strings.TrimSpace(in.Title)
	switch {
	case title == "" || len(title) > MaxTitle:
		return nil, fmt.Errorf("%w: title is required (at most %d characters)", domain.ErrInvalidInput, MaxTitle)
	case len(in.Intent) > MaxIntent:
		return nil, fmt.Errorf("%w: intent is at most %d characters", domain.ErrInvalidInput, MaxIntent)
	case (in.Content == nil) == !in.FromDraft:
		return nil, fmt.Errorf("%w: give either content or from_draft", domain.ErrInvalidInput)
	}
	var content Content
	if in.FromDraft {
		d, err := s.policy.Draft(ctx)
		if err != nil {
			return nil, err
		}
		content = Content{Rules: d.Rules, DefaultAction: d.DefaultAction}
	} else {
		content = *in.Content
	}
	content, err := normalize(content)
	if err != nil {
		return nil, err
	}
	rev, err := s.newRevision(ctx, content, actor)
	if err != nil {
		return nil, err
	}
	cr := &ChangeRequest{ID: uuid.New(), Title: title, Intent: in.Intent, Workflow: WorkflowReviewOnly, State: StateDraft, Revision: 1,
		RequestedBy: actor, CreatedAt: rev.CreatedAt, UpdatedAt: rev.CreatedAt}
	rev.ChangeRequestID, rev.Revision = cr.ID, 1
	src := "content"
	if in.FromDraft {
		src = "policy_draft"
	}
	a := s.event(ctx, "change.create", cr.ID, map[string]string{"revision": "1", "content_digest": rev.ContentDigest,
		"base_version": fmt.Sprint(rev.BaseVersionNumber), "source": src, "workflow": WorkflowReviewOnly})
	if err := s.repo.Create(ctx, cr, rev, a); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, cr.ID)
}

// Edit replaces the content as a new revision if expected is the current
// revision. The new revision is based on the active version at this moment.
func (s *Service) Edit(ctx context.Context, id uuid.UUID, expected int, c Content, actor string) (*ChangeRequest, error) {
	content, err := normalize(c)
	if err != nil {
		return nil, err
	}
	rev, err := s.newRevision(ctx, content, actor)
	if err != nil {
		return nil, err
	}
	a := s.event(ctx, "change.edit", id, map[string]string{"expected_revision": fmt.Sprint(expected),
		"revision": fmt.Sprint(expected + 1), "content_digest": rev.ContentDigest, "base_version": fmt.Sprint(rev.BaseVersionNumber)})
	return s.repo.Edit(ctx, id, expected, rev, a)
}

// Cancel cancels the change request if expected is the current revision.
func (s *Service) Cancel(ctx context.Context, id uuid.UUID, expected int, actor string) (*ChangeRequest, error) {
	a := s.event(ctx, "change.cancel", id, map[string]string{"revision": fmt.Sprint(expected), "by": actor})
	return s.repo.Cancel(ctx, id, expected, a)
}

// EnvironmentDigest fingerprints a segment environment.
func EnvironmentDigest(env policyver.SegmentEnvironment) string {
	b, _ := json.Marshal(env) // map keys are sorted by encoding/json
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Validate validates the given revision (which must be current) against the
// environment as it is now, and records the result.
func (s *Service) Validate(ctx context.Context, id uuid.UUID, revision int, actor string) (*ChangeRequest, *Validation, error) {
	rev, err := s.repo.Revision(ctx, id, revision)
	if err != nil {
		return nil, nil, err
	}
	env := s.policy.Environment(ctx)
	res := s.policy.ValidateAgainst(rev.Content.Rules, rev.Content.DefaultAction, env)
	v := &Validation{ID: uuid.New(), ChangeRequestID: id, Revision: revision, ContentDigest: rev.ContentDigest,
		EnvironmentDigest: EnvironmentDigest(env), Valid: res.Valid, Result: res, CreatedBy: actor, CreatedAt: s.now().UTC()}
	a := s.event(ctx, "change.validate", id, map[string]string{"revision": fmt.Sprint(revision), "content_digest": v.ContentDigest,
		"environment_digest": v.EnvironmentDigest, "valid": fmt.Sprint(v.Valid)})
	cr, err := s.repo.RecordValidation(ctx, v, a)
	if err != nil {
		return nil, nil, err
	}
	return cr, v, nil
}

// List lists change requests.
func (s *Service) List(ctx context.Context, f ListFilter) ([]ChangeRequest, error) {
	return s.repo.List(ctx, f)
}

// Revisions lists a change request's revisions.
func (s *Service) Revisions(ctx context.Context, id uuid.UUID) ([]Revision, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.Revisions(ctx, id)
}

// Revision returns one revision.
func (s *Service) Revision(ctx context.Context, id uuid.UUID, n int) (*Revision, error) {
	return s.repo.Revision(ctx, id, n)
}

// Drift is what changed since a revision or validation was recorded.
type Drift struct {
	State   string   `json:"state"` // current | stale
	Reasons []string `json:"reasons"`
}

// ValidationView is a validation with its staleness now.
type ValidationView struct {
	Validation
	Drift Drift `json:"drift"`
}

// SimulationView is a simulation (without rows) with its staleness now.
type SimulationView struct {
	Simulation
	Drift Drift `json:"drift"`
}

// Detail is a change request with its current content, staleness,
// validations and simulations, as the API and console show it.
type Detail struct {
	ChangeRequest
	Label       string           `json:"label"`
	Content     Content          `json:"content"`
	BaseDrift   Drift            `json:"base_drift"`
	Validations []ValidationView `json:"validations"`
	Simulations []SimulationView `json:"simulations"`
}

// Get returns the change request with its current content and, for each
// validation, whether it still describes the present: same revision, same
// active policy version, same segment environment. Nothing is invalidated
// silently; staleness is reported with its reasons.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Detail, error) {
	cr, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	rev, err := s.repo.Revision(ctx, id, cr.Revision)
	if err != nil {
		return nil, err
	}
	vals, err := s.repo.Validations(ctx, id)
	if err != nil {
		return nil, err
	}
	active, err := s.policy.Active(ctx)
	if err != nil {
		return nil, err
	}
	sims, err := s.repo.Simulations(ctx, id)
	if err != nil {
		return nil, err
	}
	envDigest := EnvironmentDigest(s.policy.Environment(ctx))
	d := &Detail{ChangeRequest: *cr, Label: Label, Content: rev.Content, Validations: make([]ValidationView, 0, len(vals)),
		Simulations: make([]SimulationView, 0, len(sims))}
	d.BaseDrift = Drift{State: "current", Reasons: []string{}}
	if active.ID != cr.BaseVersionID {
		d.BaseDrift = Drift{State: "stale", Reasons: []string{fmt.Sprintf(
			"the active policy is now version %d; this revision was written against version %d (edit the content to rebase it)",
			active.Number, cr.BaseVersionNumber)}}
	}
	for _, v := range vals {
		dr := Drift{State: "current", Reasons: []string{}}
		if v.Revision != cr.Revision {
			dr.Reasons = append(dr.Reasons, fmt.Sprintf("validated revision %d; the change request is at revision %d", v.Revision, cr.Revision))
		}
		if v.EnvironmentDigest != envDigest {
			dr.Reasons = append(dr.Reasons, "switch segment mappings changed since this validation")
		}
		if active.ID != cr.BaseVersionID && v.Revision == cr.Revision {
			dr.Reasons = append(dr.Reasons, "the active policy version changed since this revision was written")
		}
		if len(dr.Reasons) > 0 {
			dr.State = "stale"
		}
		d.Validations = append(d.Validations, ValidationView{Validation: v, Drift: dr})
	}
	for _, sim := range sims {
		set, err := s.repo.InputSet(ctx, id, sim.InputSetID)
		if err != nil {
			return nil, err
		}
		dr := Drift{State: "current", Reasons: []string{}}
		if sim.Revision != cr.Revision {
			dr.Reasons = append(dr.Reasons, fmt.Sprintf("simulated revision %d; the change request is at revision %d", sim.Revision, cr.Revision))
		}
		if set.ActiveVersionID != active.ID {
			dr.Reasons = append(dr.Reasons, fmt.Sprintf("replayed against active version %d; the active version is now %d",
				set.ActiveVersionNumber, active.Number))
		}
		if set.EnvironmentDigest != envDigest {
			dr.Reasons = append(dr.Reasons, "switch segment mappings changed since the inputs were captured")
		}
		if set.CatalogDigest != catalogDigest() {
			dr.Reasons = append(dr.Reasons, "the input catalog changed since the inputs were captured")
		}
		if len(dr.Reasons) > 0 {
			dr.State = "stale"
		}
		d.Simulations = append(d.Simulations, SimulationView{Simulation: sim, Drift: dr})
	}
	return d, nil
}

// CaseLimit bounds the recorded cases one input set captures.
const CaseLimit = 1000

func catalogDigest() string { return mustDigest(domain.InputCatalog) }

// Simulate captures a NEW frozen input set (recorded decision inputs and
// session overrides copied, active version and environment fixed) and
// replays the given revision over it. The revision must be current and
// validated. A refresh is another call: it never updates an earlier input
// set or simulation.
func (s *Service) Simulate(ctx context.Context, id uuid.UUID, revision int, actor string) (*ChangeRequest, *Simulation, error) {
	rev, err := s.repo.Revision(ctx, id, revision)
	if err != nil {
		return nil, nil, err
	}
	active, err := s.policy.Active(ctx)
	if err != nil {
		return nil, nil, err
	}
	// PostgreSQL keeps microseconds: truncate, so the stored input set
	// digests (and reproduces) exactly as it was computed here.
	now := s.now().UTC().Truncate(time.Microsecond)
	since := now.Add(-s.policy.PreviewWindow())
	recorded, err := s.policy.RecordedCases(ctx, since, CaseLimit)
	if err != nil {
		return nil, nil, err
	}
	env := s.policy.Environment(ctx)
	set := &InputSet{ID: uuid.New(), ChangeRequestID: id, Revision: revision, ContentDigest: rev.ContentDigest,
		ActiveVersionID: active.ID, ActiveVersionNumber: active.Number, ActiveVersionDigest: active.Digest,
		Environment: env, EnvironmentDigest: EnvironmentDigest(env), Catalog: domain.InputCatalog, CatalogDigest: catalogDigest(),
		WindowSince: since, CaseLimit: CaseLimit, Cases: freeze(recorded), CreatedBy: actor, CreatedAt: now}
	set.Digest = inputSetDigestOf(set)
	rows, sum, cov, out, err := Replay(set, active, rev.Content)
	if err != nil {
		return nil, nil, err
	}
	sim := &Simulation{ID: uuid.New(), ChangeRequestID: id, Revision: revision, ContentDigest: rev.ContentDigest, InputSetID: set.ID,
		InputSetDigest: set.Digest, Rows: rows, Summary: sum, Coverage: cov, OutputDigest: out, Label: SimulationLabel,
		CreatedBy: actor, CreatedAt: now}
	a := s.event(ctx, "change.simulate", id, map[string]string{"revision": fmt.Sprint(revision), "content_digest": rev.ContentDigest,
		"input_set_digest": set.Digest, "output_digest": out, "active_version": fmt.Sprint(active.Number),
		"cases": fmt.Sprint(len(set.Cases))})
	cr, err := s.repo.RecordSimulation(ctx, set, sim, a)
	if err != nil {
		return nil, nil, err
	}
	return cr, sim, nil
}

// Simulation returns one simulation with its rows.
func (s *Service) Simulation(ctx context.Context, id, simID uuid.UUID) (*Simulation, error) {
	return s.repo.Simulation(ctx, id, simID)
}

// InputSet returns a simulation's frozen input set.
func (s *Service) InputSet(ctx context.Context, id, setID uuid.UUID) (*InputSet, error) {
	return s.repo.InputSet(ctx, id, setID)
}

// Reproduction is the result of re-running a stored simulation from its own
// frozen input set.
type Reproduction struct {
	SimulationID         uuid.UUID `json:"simulation_id"`
	InputSetDigest       string    `json:"input_set_digest"`
	InputSetDigestMatch  bool      `json:"input_set_digest_matches"`
	StoredOutputDigest   string    `json:"stored_output_digest"`
	ReproducedOutputHash string    `json:"reproduced_output_digest"`
	Matches              bool      `json:"matches"`
	Note                 string    `json:"note"`
}

// Reproduce re-runs a stored simulation from its frozen input set, its
// immutable revision and the immutable active version it names, without
// reading any live state, and compares the canonical output digests. It
// records nothing.
func (s *Service) Reproduce(ctx context.Context, id, simID uuid.UUID) (*Reproduction, error) {
	sim, err := s.repo.Simulation(ctx, id, simID)
	if err != nil {
		return nil, err
	}
	set, err := s.repo.InputSet(ctx, id, sim.InputSetID)
	if err != nil {
		return nil, err
	}
	rev, err := s.repo.Revision(ctx, id, sim.Revision)
	if err != nil {
		return nil, err
	}
	active, err := s.policy.Version(ctx, set.ActiveVersionNumber)
	if err != nil {
		return nil, err
	}
	_, _, _, out, err := Replay(set, active, rev.Content)
	if err != nil {
		return nil, err
	}
	r := &Reproduction{SimulationID: sim.ID, InputSetDigest: set.Digest, InputSetDigestMatch: inputSetDigestOf(set) == set.Digest,
		StoredOutputDigest: sim.OutputDigest, ReproducedOutputHash: out}
	r.Matches = r.InputSetDigestMatch && out == sim.OutputDigest
	r.Note = "Re-run from the stored input set only (no live state read)."
	if !r.Matches {
		r.Note = "MISMATCH: the stored input set or output no longer reproduces; treat this simulation as untrustworthy."
	}
	return r, nil
}
