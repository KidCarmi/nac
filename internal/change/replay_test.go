package change

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
)

func replayFixture(t *testing.T) (*InputSet, *domain.PolicyVersion, Content) {
	t.Helper()
	at := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	bound := domain.Evidence{Value: "true", Source: domain.SourceEAPTLS, Authenticated: true, Status: domain.EvidencePresent, ObservedAt: &at}
	unbound := domain.Evidence{Value: "false", Source: domain.SourceEAPTLS, Authenticated: true, Status: domain.EvidencePresent, ObservedAt: &at}
	staleP := domain.Evidence{Value: "compliant", Source: "endpoint_record", Status: domain.EvidenceStale, ObservedAt: &at}
	s1, s2, s3 := uuid.New(), uuid.New(), uuid.New()
	recorded := []policyver.RecordedCase{
		{DecisionID: uuid.New(), SessionID: &s2, Subject: "b", OccurredAt: at, Inputs: domain.Inputs{"identity.bound": unbound, "posture": staleP},
			RecordedAction: domain.ActionDeny},
		{DecisionID: uuid.New(), SessionID: &s1, Subject: "a", OccurredAt: at, Inputs: domain.Inputs{"identity.bound": bound},
			RecordedAction: domain.ActionAllow, RecordedSegment: "corp"},
		{DecisionID: uuid.New(), SessionID: &s3, Subject: "c", OccurredAt: at, Inputs: domain.Inputs{"identity.bound": bound},
			RecordedAction: domain.ActionAllow, RecordedSegment: "corp", Override: domain.CoAQuarantine},
		{DecisionID: uuid.New(), Subject: "no session", OccurredAt: at}, // not replayable: no session
	}
	corp := domain.Policy{ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("corp")), Name: "corp", Priority: 10, Enabled: true,
		Action: domain.ActionAllow, Segment: "corp",
		Conditions: []domain.Condition{{Field: "identity.bound", Operator: domain.OpEquals, Values: []string{"true"}}}}
	active := &domain.PolicyVersion{ID: uuid.New(), Number: 3, Rules: []domain.Policy{corp}, DefaultAction: domain.ActionDeny}
	active.Digest = policyver.Digest(active.Rules, active.DefaultAction)
	guest := corp
	guest.ID, guest.Name, guest.Segment = uuid.NewSHA1(uuid.NameSpaceOID, []byte("guest")), "guest", "guest"
	content := Content{Rules: []domain.Policy{guest}, DefaultAction: domain.ActionDeny}
	set := &InputSet{Revision: 2, ContentDigest: content.Digest(), ActiveVersionID: active.ID, ActiveVersionNumber: 3,
		ActiveVersionDigest: active.Digest, Environment: policyver.SegmentEnvironment{Checked: true, Maps: map[string]map[string]int{}},
		CatalogDigest: catalogDigest(), WindowSince: at.Add(-time.Hour), CaseLimit: 10, Cases: freeze(recorded)}
	set.Digest = inputSetDigestOf(set)
	// The recorded cases are copied: changing them now cannot change the set.
	recorded[1].Inputs["identity.bound"] = unbound
	recorded[2].Override = ""
	return set, active, content
}

func TestReplay_ReproducibleAndOrdered(t *testing.T) {
	set, active, content := replayFixture(t)
	if len(set.Cases) != 3 {
		t.Fatalf("frozen %d cases, want 3 (a case without a session is not replayable)", len(set.Cases))
	}
	if inputSetDigestOf(set) != set.Digest {
		t.Fatal("input-set digest changed after the source cases were mutated: inputs were not copied")
	}
	rows, sum, _, out1, err := Replay(set, active, content)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1].SessionID.String() > rows[i].SessionID.String() {
			t.Fatal("rows are not in canonical session order")
		}
	}
	for i := 0; i < 5; i++ {
		if _, _, _, out, _ := Replay(set, active, content); out != out1 {
			t.Fatal("replaying the same input set gave a different output digest")
		}
	}
	// a: corp -> guest (changed); b: deny -> deny, stale posture recorded;
	// c: quarantined by override under both (no effective change).
	if sum.CasesReplayed != 3 || sum.Changed != 1 || sum.PolicyChanged != 2 || sum.OverrideApplies != 1 || sum.StaleEvidence != 1 {
		t.Fatalf("summary: %+v", sum)
	}
}

func TestReplay_RefusesOtherVersionOrContent(t *testing.T) {
	set, active, content := replayFixture(t)
	other := *active
	other.Digest = "other"
	if _, _, _, _, err := Replay(set, &other, content); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("replay against another active version: %v", err)
	}
	content.DefaultAction = domain.ActionQuarantine
	if _, _, _, _, err := Replay(set, active, content); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("replay of other content: %v", err)
	}
}

func TestCanonicalDigest_KeyOrderIndependent(t *testing.T) {
	a, _ := CanonicalDigest(map[string]any{"b": 1, "a": "<x>"})
	b, _ := CanonicalDigest(map[string]any{"a": "<x>", "b": 1})
	if a != b {
		t.Fatal("canonical digest depends on key order")
	}
	j, _ := canonicalJSON(map[string]any{"b": 1, "a": "<x>"})
	if string(j) != `{"a":"<x>","b":1}` {
		t.Fatalf("canonical JSON: %s", j)
	}
}
