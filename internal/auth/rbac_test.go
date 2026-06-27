package auth

import (
	"testing"

	"github.com/kidcarmi/nac/internal/domain"
)

func TestRBACMatrix(t *testing.T) {
	cases := []struct {
		role domain.Role
		perm Permission
		want bool
	}{
		{domain.RoleAdmin, PermOperatorManage, true},
		{domain.RoleOperator, PermOperatorManage, false},
		{domain.RoleOperator, PermPolicyWrite, true},
		{domain.RoleViewer, PermPolicyWrite, false},
		{domain.RoleViewer, PermEndpointRead, true},
		{domain.RoleViewer, PermAccessEvaluate, false},
		{domain.Role("bogus"), PermEndpointRead, false},
	}
	for _, tc := range cases {
		if got := HasPermission(tc.role, tc.perm); got != tc.want {
			t.Errorf("HasPermission(%s,%s)=%v want %v", tc.role, tc.perm, got, tc.want)
		}
	}
}

func TestPrincipalCan_NilFailsClosed(t *testing.T) {
	var p *Principal
	if p.Can(PermEndpointRead) {
		t.Fatalf("nil principal must not have permissions")
	}
}
