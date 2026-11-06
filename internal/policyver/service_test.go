package policyver

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// memStore is an in-memory Store with the same atomicity contract (a mutex
// stands in for the transaction lock).
type memStore struct {
	mu          sync.Mutex
	ws          []domain.Policy
	def         domain.PolicyAction
	versions    []domain.PolicyVersion
	activations []domain.PolicyActivation
	segments    map[string]map[string]int
}

// view mirrors what the Postgres store loads inside the transaction.
func (m *memStore) view() TxView {
	n := 1
	for _, v := range m.versions {
		if v.Number >= n {
			n = v.Number + 1
		}
	}
	return TxView{NextNumber: n, Segments: m.segments}
}

func (m *memStore) Workspace(context.Context) ([]domain.Policy, domain.PolicyAction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Policy(nil), m.ws...), m.def, nil
}
func (m *memStore) SetDraftDefaultAction(_ context.Context, a domain.PolicyAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.def = a
	return nil
}
func (m *memStore) activeLocked() *domain.PolicyVersion {
	if len(m.activations) == 0 {
		return nil
	}
	id := m.activations[len(m.activations)-1].VersionID
	for i := range m.versions {
		if m.versions[i].ID == id {
			v := m.versions[i]
			v.Active = true
			return &v
		}
	}
	return nil
}
func (m *memStore) ActiveVersion(context.Context) (*domain.PolicyVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v := m.activeLocked(); v != nil {
		return v, nil
	}
	return nil, domain.ErrNotFound
}
func (m *memStore) GetVersion(_ context.Context, n int) (*domain.PolicyVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.versions {
		if v.Number == n {
			return &v, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (m *memStore) ListVersions(context.Context) ([]domain.PolicyVersion, error) {
	return append([]domain.PolicyVersion(nil), m.versions...), nil // called under lock by Publish
}
func (m *memStore) ListActivations(context.Context, int) ([]domain.PolicyActivation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.PolicyActivation(nil), m.activations...), nil
}
func (m *memStore) PublishWorkspace(_ context.Context, prepare func([]domain.Policy, domain.PolicyAction, *domain.PolicyVersion, TxView) (*domain.PolicyVersion, *domain.PolicyActivation, error)) (*domain.PolicyVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, a, err := prepare(append([]domain.Policy(nil), m.ws...), m.def, m.activeLocked(), m.view())
	if err != nil {
		return nil, err
	}
	m.versions = append(m.versions, *v)
	a.VersionID, a.VersionNumber = v.ID, v.Number
	m.activations = append(m.activations, *a)
	return v, nil
}
func (m *memStore) Activate(_ context.Context, n int, prepare func(target, active *domain.PolicyVersion, view TxView) (*domain.PolicyActivation, error), reset bool) (*domain.PolicyActivation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var target *domain.PolicyVersion
	for i := range m.versions {
		if m.versions[i].Number == n {
			target = &m.versions[i]
		}
	}
	if target == nil {
		return nil, domain.ErrNotFound
	}
	a, err := prepare(target, m.activeLocked(), m.view())
	if err != nil {
		return nil, err
	}
	a.VersionID, a.VersionNumber = target.ID, target.Number
	m.activations = append(m.activations, *a)
	if reset {
		m.ws = append([]domain.Policy(nil), target.Rules...)
		m.def = target.DefaultAction
	}
	return a, nil
}

type fixedSegments map[string]map[string]int

func (f fixedSegments) SegmentMappings(context.Context) (map[string]map[string]int, error) {
	return f, nil
}

type fixedCases []RecordedCase

func (f fixedCases) RecentCases(context.Context, time.Time, int) ([]RecordedCase, error) {
	return f, nil
}

func rule(name string, prio int, action domain.PolicyAction, seg string, conds ...domain.Condition) domain.Policy {
	return domain.Policy{ID: uuid.New(), Name: name, Priority: prio, Enabled: true, Action: action, Segment: seg, Conditions: conds}
}

var lab = fixedSegments{"sw1": {"corp": 10, "quarantine": 20}}

func boundIdentity() domain.Condition {
	return domain.Condition{Field: "identity.bound", Operator: domain.OpEquals, Values: []string{"true"}}
}

func newSvc(t *testing.T, ws ...domain.Policy) (*Service, *memStore) {
	t.Helper()
	st := &memStore{ws: ws, segments: lab}
	svc := New(st, nil, lab, domain.ActionDeny)
	if err := svc.EnsureInitialVersion(context.Background()); err != nil {
		t.Fatalf("initial version: %v", err)
	}
	return svc, st
}

func publishDraft(t *testing.T, svc *Service, note string) *domain.PolicyVersion {
	t.Helper()
	d, err := svc.Draft(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Publish(context.Background(), PublishInput{ExpectedDigest: d.Digest, Note: note, Actor: "admin@nac.local"})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return v
}

func TestEnsureInitialVersion_IsIdempotent(t *testing.T) {
	svc, st := newSvc(t, rule("allow-bound", 10, domain.ActionAllow, "corp", boundIdentity()))
	if err := svc.EnsureInitialVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(st.versions) != 1 || st.activations[0].Kind != domain.ActivationInitial {
		t.Fatalf("want exactly one initial version, got %d", len(st.versions))
	}
	rs, _ := svc.ActiveRuleSet(context.Background())
	if rs.VersionNumber != 1 || len(rs.Rules) != 1 {
		t.Fatalf("active rule set = %+v", rs)
	}
}

func TestPublish_RequiresPreviewedDigest(t *testing.T) {
	svc, st := newSvc(t, rule("allow-bound", 10, domain.ActionAllow, "corp", boundIdentity()))
	ctx := context.Background()
	d, _ := svc.Draft(ctx)

	// A concurrent edit after preview must make publication fail.
	st.ws = append(st.ws, rule("quarantine-rest", 100, domain.ActionQuarantine, "quarantine"))
	_, err := svc.Publish(ctx, PublishInput{ExpectedDigest: d.Digest, Actor: "a"})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict on stale digest, got %v", err)
	}
	if _, err := svc.Publish(ctx, PublishInput{Actor: "a"}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected missing digest to be rejected, got %v", err)
	}
}

func TestPublish_RejectsIdenticalAndInvalidDrafts(t *testing.T) {
	svc, st := newSvc(t, rule("allow-bound", 10, domain.ActionAllow, "corp", boundIdentity()))
	ctx := context.Background()
	d, _ := svc.Draft(ctx)
	if d.DiffersFromActive {
		t.Fatalf("fresh draft should equal active version")
	}
	if _, err := svc.Publish(ctx, PublishInput{ExpectedDigest: d.Digest}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected conflict for identical draft, got %v", err)
	}

	st.ws = append(st.ws, rule("allow-guest", 20, domain.ActionAllow, "guest", boundIdentity()))
	d, _ = svc.Draft(ctx)
	if d.Validation.Valid {
		t.Fatalf("segment without VLAN mapping must be invalid")
	}
	if _, err := svc.Publish(ctx, PublishInput{ExpectedDigest: d.Digest}); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid draft to be rejected, got %v", err)
	}
	if len(st.versions) != 1 {
		t.Fatalf("no version may be created by a rejected publish")
	}
}

func TestPublishAndRestore_PreserveHistory(t *testing.T) {
	ctx := context.Background()
	allow := rule("allow-bound", 10, domain.ActionAllow, "corp", boundIdentity())
	svc, st := newSvc(t, allow)

	// v2: quarantine everyone bound.
	st.ws[0].Action, st.ws[0].Segment = domain.ActionQuarantine, "quarantine"
	v2 := publishDraft(t, svc, "quarantine all")
	if v2.Number != 2 {
		t.Fatalf("expected version 2, got %d", v2.Number)
	}

	act, err := svc.Restore(ctx, RestoreInput{Number: 1, ResetDraft: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if act.Kind != domain.ActivationRestore || act.VersionNumber != 1 {
		t.Fatalf("activation = %+v", act)
	}
	rs, _ := svc.ActiveRuleSet(ctx)
	if rs.VersionNumber != 1 || rs.Rules[0].Action != domain.ActionAllow {
		t.Fatalf("restore did not re-activate v1: %+v", rs)
	}
	// v2 still exists unchanged; history has initial, publish, restore.
	got, _ := svc.Version(ctx, 2)
	if got.Rules[0].Action != domain.ActionQuarantine {
		t.Fatalf("published version must be immutable")
	}
	if len(st.activations) != 3 {
		t.Fatalf("want 3 activations, got %d", len(st.activations))
	}
	d, _ := svc.Draft(ctx)
	if d.DiffersFromActive {
		t.Fatalf("ResetDraft should make the draft equal the restored version")
	}
	if _, err := svc.Restore(ctx, RestoreInput{Number: 1}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("restoring the active version should conflict, got %v", err)
	}
}

func TestPreview_ReportsChangedAndInsufficientEvidence(t *testing.T) {
	ctx := context.Background()
	svc, st := newSvc(t, rule("allow-bound", 10, domain.ActionAllow, "corp", boundIdentity()))

	compliant := domain.Inputs{}
	compliant.Set("identity.bound", "true", domain.SourceBinding, nil, true)
	compliant.Set("posture", "compliant", domain.SourcePosture, nil, false)
	noPosture := domain.Inputs{}
	noPosture.Set("identity.bound", "true", domain.SourceBinding, nil, true)
	svc.SetSources(fixedCases{
		{DecisionID: uuid.New(), Subject: "laptop-a", Inputs: compliant, RecordedAction: domain.ActionAllow},
		{DecisionID: uuid.New(), Subject: "laptop-b", Inputs: noPosture, RecordedAction: domain.ActionAllow},
	}, lab)

	// Draft: require compliant posture for corp, otherwise quarantine.
	st.ws = []domain.Policy{
		rule("allow-compliant", 10, domain.ActionAllow, "corp", boundIdentity(),
			domain.Condition{Field: "posture", Operator: domain.OpEquals, Values: []string{"compliant"}}),
		rule("quarantine-bound", 20, domain.ActionQuarantine, "quarantine", boundIdentity()),
	}
	p, err := svc.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Label != PreviewLabel || p.CasesExamined != 2 {
		t.Fatalf("preview header = %+v", p)
	}
	if p.Changed != 1 || p.Insufficient != 1 || len(p.Rows) != 1 {
		t.Fatalf("want 1 changed / 1 insufficient row, got changed=%d insufficient=%d rows=%d", p.Changed, p.Insufficient, len(p.Rows))
	}
	row := p.Rows[0]
	if row.Case.Subject != "laptop-b" || row.DraftAction != domain.ActionQuarantine || row.InsufficientEvidence[0] != "posture" {
		t.Fatalf("unexpected row: %+v", row)
	}
}

func TestValidate_Errors(t *testing.T) {
	svc, _ := newSvc(t)
	ctx := context.Background()
	cases := map[string][]domain.Policy{
		"duplicate names":   {rule("a", 1, domain.ActionDeny, ""), rule("A", 2, domain.ActionDeny, "")},
		"allow w/o segment": {rule("a", 1, domain.ActionAllow, "", boundIdentity())},
		"unknown field": {rule("a", 1, domain.ActionDeny, "",
			domain.Condition{Field: "postrue", Operator: domain.OpExists})},
	}
	for name, rules := range cases {
		if v := svc.Validate(ctx, rules, domain.ActionDeny); v.Valid {
			t.Fatalf("%s: expected invalid", name)
		}
	}
	shadow := []domain.Policy{rule("all", 1, domain.ActionDeny, ""), rule("later", 2, domain.ActionAllow, "corp", boundIdentity())}
	if v := svc.Validate(ctx, shadow, domain.ActionDeny); !v.Valid || len(v.Warnings) == 0 {
		t.Fatalf("shadowed rule should be a warning, got %+v", v)
	}
}
