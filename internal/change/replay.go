package change

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
	"github.com/kidcarmi/nac/internal/policyver"
)

// SimulationLabel is stored with, and shown beside, every simulation.
const SimulationLabel = "Historical replay of recorded decision inputs: a prediction, not live-network verification. " +
	"It says nothing about current switch state or endpoint reachability; actual decisions are made at the next authentication."

// FrozenCase is one recorded decision copied into an input set. Inputs and
// the session override are COPIED (the session row and its override can
// change); the decision ID is kept as an immutable reference.
type FrozenCase struct {
	DecisionID      uuid.UUID           `json:"decision_id"`
	SessionID       uuid.UUID           `json:"session_id"`
	Subject         string              `json:"subject"`
	OccurredAt      time.Time           `json:"occurred_at"`
	Inputs          domain.Inputs       `json:"inputs"`
	InputsDigest    string              `json:"inputs_digest"`
	RecordedAction  domain.PolicyAction `json:"recorded_action"`
	RecordedSegment string              `json:"recorded_segment,omitempty"`
	RecordedVersion int                 `json:"recorded_version,omitempty"`
	Override        string              `json:"override,omitempty"`
}

// InputSet is everything a simulation reads, persisted before it runs.
type InputSet struct {
	ID                  uuid.UUID                    `json:"id"`
	ChangeRequestID     uuid.UUID                    `json:"change_request_id"`
	Revision            int                          `json:"revision"`
	ContentDigest       string                       `json:"content_digest"`
	Digest              string                       `json:"input_set_digest"`
	ActiveVersionID     uuid.UUID                    `json:"active_version_id"`
	ActiveVersionNumber int                          `json:"active_version_number"`
	ActiveVersionDigest string                       `json:"active_version_digest"`
	Environment         policyver.SegmentEnvironment `json:"environment"`
	EnvironmentDigest   string                       `json:"environment_digest"`
	Catalog             []domain.InputSpec           `json:"catalog"`
	CatalogDigest       string                       `json:"catalog_digest"`
	WindowSince         time.Time                    `json:"window_since"`
	CaseLimit           int                          `json:"case_limit"`
	Cases               []FrozenCase                 `json:"cases"`
	CreatedBy           string                       `json:"created_by"`
	CreatedAt           time.Time                    `json:"created_at"`
}

// SimRow is one replayed case.
type SimRow struct {
	DecisionID      uuid.UUID           `json:"decision_id"`
	SessionID       uuid.UUID           `json:"session_id"`
	Subject         string              `json:"subject"`
	ActiveAction    domain.PolicyAction `json:"active_action"`
	ActiveSegment   string              `json:"active_segment,omitempty"`
	ActiveRule      string              `json:"active_rule,omitempty"`
	ProposedAction  domain.PolicyAction `json:"proposed_action"`
	ProposedSegment string              `json:"proposed_segment,omitempty"`
	ProposedRule    string              `json:"proposed_rule,omitempty"`
	Changed         bool                `json:"changed"`
	PolicyChanged   bool                `json:"policy_changed"`
	OverrideApplies bool                `json:"override_applies,omitempty"`
	MissingInputs   []string            `json:"missing_inputs"`
	StaleInputs     []string            `json:"stale_inputs"`
}

// Summary counts the replay.
type Summary struct {
	CasesReplayed   int `json:"cases_replayed"`
	Changed         int `json:"changed"`
	PolicyChanged   int `json:"policy_changed"`
	MissingEvidence int `json:"missing_evidence"`
	StaleEvidence   int `json:"stale_evidence"`
	OverrideApplies int `json:"override_applies"`
}

// Coverage says what the replay could and could not see.
type Coverage struct {
	WindowSince  time.Time `json:"window_since"`
	CaseLimit    int       `json:"case_limit"`
	CasesInSet   int       `json:"cases_in_set"`
	LimitReached bool      `json:"limit_reached"`
	Notes        []string  `json:"notes"`
}

// Simulation is an immutable replay of one revision over one input set.
type Simulation struct {
	ID              uuid.UUID `json:"id"`
	ChangeRequestID uuid.UUID `json:"change_request_id"`
	Revision        int       `json:"revision"`
	ContentDigest   string    `json:"content_digest"`
	InputSetID      uuid.UUID `json:"input_set_id"`
	InputSetDigest  string    `json:"input_set_digest"`
	Rows            []SimRow  `json:"rows"`
	Summary         Summary   `json:"summary"`
	Coverage        Coverage  `json:"coverage"`
	OutputDigest    string    `json:"output_digest"`
	Label           string    `json:"label"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
}

// CanonicalDigest is SHA-256 over the canonical JSON of v: object keys
// sorted, no insignificant whitespace, no HTML escaping, numbers kept as
// written. For the ASCII keys and string/integer values used here this is
// the RFC 8785 (JCS) serialization.
func CanonicalDigest(v any) (string, error) {
	b, err := canonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil { // maps encode with sorted keys
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func mustDigest(v any) string {
	d, err := CanonicalDigest(v)
	if err != nil {
		panic(fmt.Sprintf("canonical digest: %v", err)) // only plain data reaches here
	}
	return d
}

// inputSetDigestOf identifies an input set by its contents (not its ID or
// creation time).
func inputSetDigestOf(s *InputSet) string {
	return mustDigest(struct {
		Revision            int                          `json:"revision"`
		ContentDigest       string                       `json:"content_digest"`
		ActiveVersionID     uuid.UUID                    `json:"active_version_id"`
		ActiveVersionDigest string                       `json:"active_version_digest"`
		Environment         policyver.SegmentEnvironment `json:"environment"`
		CatalogDigest       string                       `json:"catalog_digest"`
		WindowSince         time.Time                    `json:"window_since"`
		CaseLimit           int                          `json:"case_limit"`
		Cases               []FrozenCase                 `json:"cases"`
	}{s.Revision, s.ContentDigest, s.ActiveVersionID, s.ActiveVersionDigest, s.Environment, s.CatalogDigest, s.WindowSince,
		s.CaseLimit, s.Cases})
}

// freeze copies recorded cases into an input set, in canonical order.
func freeze(cases []policyver.RecordedCase) []FrozenCase {
	out := make([]FrozenCase, 0, len(cases))
	for _, c := range cases {
		if c.SessionID == nil {
			continue
		}
		in := domain.Inputs{}
		for k, v := range c.Inputs {
			in[k] = v
		}
		out = append(out, FrozenCase{DecisionID: c.DecisionID, SessionID: *c.SessionID, Subject: c.Subject,
			OccurredAt: c.OccurredAt.UTC(), Inputs: in, InputsDigest: mustDigest(in), RecordedAction: c.RecordedAction,
			RecordedSegment: c.RecordedSegment, RecordedVersion: c.RecordedVersion, Override: c.Override})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SessionID != out[j].SessionID {
			return out[i].SessionID.String() < out[j].SessionID.String()
		}
		return out[i].DecisionID.String() < out[j].DecisionID.String()
	})
	return out
}

// Replay evaluates every frozen case under the active version and the
// proposed content. It is a pure function of the input set, the active
// version (which must be the one the set names) and the content, so running
// it again on the same input set gives byte-identical canonical output.
func Replay(set *InputSet, active *domain.PolicyVersion, content Content) (rows []SimRow, sum Summary, cov Coverage, outputDigest string, err error) {
	if active.ID != set.ActiveVersionID || active.Digest != set.ActiveVersionDigest {
		return nil, Summary{}, Coverage{}, "", fmt.Errorf("%w: the input set names active version %d (%s); got version %d (%s)",
			domain.ErrConflict, set.ActiveVersionNumber, set.ActiveVersionDigest, active.Number, active.Digest)
	}
	if content.Digest() != set.ContentDigest {
		return nil, Summary{}, Coverage{}, "", fmt.Errorf("%w: the input set was captured for content %s", domain.ErrConflict, set.ContentDigest)
	}
	rows = make([]SimRow, 0, len(set.Cases))
	for _, fc := range set.Cases {
		sid := fc.SessionID
		r := policyver.CompareCase(policyver.RecordedCase{DecisionID: fc.DecisionID, SessionID: &sid, Subject: fc.Subject,
			OccurredAt: fc.OccurredAt, Inputs: fc.Inputs, RecordedAction: fc.RecordedAction, RecordedSegment: fc.RecordedSegment,
			RecordedVersion: fc.RecordedVersion, Override: fc.Override}, active.Rules, active.DefaultAction, content.Rules, content.DefaultAction)
		row := SimRow{DecisionID: fc.DecisionID, SessionID: fc.SessionID, Subject: fc.Subject,
			ActiveAction: r.ActiveAction, ActiveSegment: r.ActiveSegment, ActiveRule: r.ActiveRule,
			ProposedAction: r.DraftAction, ProposedSegment: r.DraftSegment, ProposedRule: r.DraftRule,
			Changed: r.Changed, PolicyChanged: r.PolicyChanged, OverrideApplies: r.OverrideApplies,
			MissingInputs: append([]string{}, r.InsufficientEvidence...), StaleInputs: append([]string{}, r.StaleInputs...)}
		sort.Strings(row.MissingInputs)
		rows = append(rows, row)
		sum.CasesReplayed++
		if row.Changed {
			sum.Changed++
		}
		if row.PolicyChanged {
			sum.PolicyChanged++
		}
		if len(row.MissingInputs) > 0 {
			sum.MissingEvidence++
		}
		if len(row.StaleInputs) > 0 {
			sum.StaleEvidence++
		}
		if row.OverrideApplies {
			sum.OverrideApplies++
		}
	}
	cov = Coverage{WindowSince: set.WindowSince, CaseLimit: set.CaseLimit, CasesInSet: len(set.Cases),
		LimitReached: len(set.Cases) >= set.CaseLimit, Notes: []string{
			"Only the newest recorded decision per session in the window is replayed; sessions with no recorded decision are not covered.",
			"Inputs are as recorded at decision time (their freshness status included); they are not re-observed.",
		}}
	if cov.LimitReached {
		cov.Notes = append(cov.Notes, fmt.Sprintf("The case limit (%d) was reached: older sessions in the window are not covered.", set.CaseLimit))
	}
	if len(set.Cases) == 0 {
		cov.Notes = append(cov.Notes, "No recorded decisions in the window: the replay covers nothing.")
	}
	outputDigest = mustDigest(struct {
		Rows     []SimRow `json:"rows"`
		Summary  Summary  `json:"summary"`
		Coverage Coverage `json:"coverage"`
	}{rows, sum, cov})
	return rows, sum, cov, outputDigest, nil
}
