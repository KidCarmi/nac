package access

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

type stubPolicies struct {
	policies []domain.Policy
	err      error
}

func (s stubPolicies) EnabledPolicies(context.Context) ([]domain.Policy, error) {
	return s.policies, s.err
}

type stubEndpoints struct {
	byMAC map[string]*domain.Endpoint
}

func (s stubEndpoints) GetByMAC(_ context.Context, mac string) (*domain.Endpoint, error) {
	e, ok := s.byMAC[mac]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return e, nil
}

func TestEvaluate_UnknownEndpointDefaultDeny(t *testing.T) {
	svc := NewService(NewEngine(), stubPolicies{}, stubEndpoints{byMAC: map[string]*domain.Endpoint{}})
	d, err := svc.Evaluate(context.Background(), EvaluateInput{MAC: "aa:bb:cc:dd:ee:ff"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if d.Action != domain.ActionDeny {
		t.Fatalf("unknown endpoint should default deny, got %q", d.Action)
	}
}

func TestEvaluate_UsesStoredEndpointPosture(t *testing.T) {
	pol := domain.Policy{
		ID: uuid.New(), Name: "compliant-laptops", Enabled: true, Priority: 10,
		Action: domain.ActionAllow, Segment: "corp",
		Conditions: []domain.Condition{
			{Field: "device_type", Operator: domain.OpEquals, Values: []string{"laptop"}},
			{Field: "posture", Operator: domain.OpEquals, Values: []string{"compliant"}},
		},
	}
	eps := stubEndpoints{byMAC: map[string]*domain.Endpoint{
		"aa:bb:cc:dd:ee:ff": {MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "laptop", Posture: domain.PostureCompliant},
	}}
	svc := NewService(NewEngine(), stubPolicies{policies: []domain.Policy{pol}}, eps)

	d, err := svc.Evaluate(context.Background(), EvaluateInput{MAC: "aa:bb:cc:dd:ee:ff"})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if d.Action != domain.ActionAllow || d.Segment != "corp" {
		t.Fatalf("expected allow/corp, got %q/%q", d.Action, d.Segment)
	}
}

func TestEvaluate_RequestPostureOverrides(t *testing.T) {
	pol := domain.Policy{
		ID: uuid.New(), Name: "block-noncompliant", Enabled: true, Priority: 10,
		Action: domain.ActionQuarantine, Segment: "remediation",
		Conditions: []domain.Condition{
			{Field: "posture", Operator: domain.OpEquals, Values: []string{"noncompliant"}},
		},
	}
	eps := stubEndpoints{byMAC: map[string]*domain.Endpoint{
		"aa:bb:cc:dd:ee:ff": {MAC: "aa:bb:cc:dd:ee:ff", Posture: domain.PostureCompliant},
	}}
	svc := NewService(NewEngine(), stubPolicies{policies: []domain.Policy{pol}}, eps)

	// Inline assessment reports the device is now noncompliant.
	d, _ := svc.Evaluate(context.Background(), EvaluateInput{MAC: "aa:bb:cc:dd:ee:ff", Posture: domain.PostureNoncompliant})
	if d.Action != domain.ActionQuarantine {
		t.Fatalf("expected quarantine from overridden posture, got %q", d.Action)
	}
}

func TestEvaluate_PropagatesPolicySourceError(t *testing.T) {
	boom := errors.New("db down")
	svc := NewService(NewEngine(), stubPolicies{err: boom}, stubEndpoints{byMAC: map[string]*domain.Endpoint{}})
	if _, err := svc.Evaluate(context.Background(), EvaluateInput{MAC: "aa:bb:cc:dd:ee:ff"}); !errors.Is(err, boom) {
		t.Fatalf("expected policy source error to propagate, got %v", err)
	}
}
