package access

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/kidcarmi/nac/internal/domain"
)

// benchPolicies builds n enabled policies, the last of which matches, to
// exercise the worst case (full scan) of the decision engine.
func benchPolicies(n int) []domain.Policy {
	ps := make([]domain.Policy, 0, n)
	for i := 0; i < n-1; i++ {
		ps = append(ps, domain.Policy{
			ID: uuid.New(), Name: fmt.Sprintf("p%d", i), Priority: i, Enabled: true,
			Action: domain.ActionDeny,
			Conditions: []domain.Condition{
				{Field: "device_type", Operator: domain.OpEquals, Values: []string{fmt.Sprintf("type-%d", i)}},
			},
		})
	}
	ps = append(ps, domain.Policy{
		ID: uuid.New(), Name: "match", Priority: n, Enabled: true,
		Action: domain.ActionAllow, Segment: "corp",
		Conditions: []domain.Condition{
			{Field: "device_type", Operator: domain.OpEquals, Values: []string{"laptop"}},
			{Field: "posture", Operator: domain.OpEquals, Values: []string{"compliant"}},
		},
	})
	return ps
}

// BenchmarkEvaluate measures the hot-path decision cost as the policy set
// grows. It is the primary input to the performance-regression gate.
func BenchmarkEvaluate(b *testing.B) {
	ep := domain.Endpoint{MAC: "aa:bb:cc:dd:ee:ff", DeviceType: "laptop", Posture: domain.PostureCompliant}
	for _, size := range []int{10, 100, 500} {
		policies := benchPolicies(size)
		eng := NewEngine()
		b.Run(fmt.Sprintf("policies=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = eng.Evaluate(ep, policies)
			}
		})
	}
}
