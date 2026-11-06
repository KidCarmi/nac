package auth

import "github.com/kidcarmi/nac/internal/domain"

// Permission is a coarse-grained capability checked at the API boundary.
type Permission string

const (
	PermEndpointRead   Permission = "endpoint:read"
	PermEndpointWrite  Permission = "endpoint:write"
	PermPolicyRead     Permission = "policy:read"
	PermPolicyWrite    Permission = "policy:write"
	PermAccessEvaluate Permission = "access:evaluate"
	PermAuditRead      Permission = "audit:read"
	PermOperatorManage Permission = "operator:manage"
	// Pilot: switches, identity bindings and session troubleshooting.
	PermNetworkRead  Permission = "network:read"
	PermNetworkWrite Permission = "network:write"
	// PermSessionAct allows CoA actions (quarantine/restore/reauthenticate).
	PermSessionAct Permission = "session:act"
)

// rolePermissions is the authoritative role→permission matrix. It is defined
// statically (not data-driven) so the security model is reviewable in one
// place and cannot be altered at runtime by untrusted input.
//
// Model:
//   - admin    : full control, including operator management and audit.
//   - operator : manage endpoints and policies; evaluate access; read audit.
//   - viewer   : read-only across endpoints, policies, and audit.
var rolePermissions = map[domain.Role]map[Permission]bool{
	domain.RoleAdmin: {
		PermEndpointRead: true, PermEndpointWrite: true,
		PermPolicyRead: true, PermPolicyWrite: true,
		PermAccessEvaluate: true,
		PermAuditRead:      true,
		PermOperatorManage: true,
		PermNetworkRead:    true, PermNetworkWrite: true, PermSessionAct: true,
	},
	domain.RoleOperator: {
		PermEndpointRead: true, PermEndpointWrite: true,
		PermPolicyRead: true, PermPolicyWrite: true,
		PermAccessEvaluate: true,
		PermAuditRead:      true,
		PermNetworkRead:    true, PermNetworkWrite: true, PermSessionAct: true,
	},
	domain.RoleViewer: {
		PermEndpointRead: true,
		PermPolicyRead:   true,
		PermAuditRead:    true,
		PermNetworkRead:  true,
	},
}

// HasPermission reports whether role grants perm. Unknown roles grant nothing
// (fail closed).
func HasPermission(role domain.Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[perm]
}

// Can is a convenience wrapper for a Principal.
func (p *Principal) Can(perm Permission) bool {
	if p == nil {
		return false
	}
	return HasPermission(p.Role, perm)
}
