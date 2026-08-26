package auth

import "strings"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type Permission string

const (
	PermissionReadDNS           Permission = "dns:read"
	PermissionMutateDNS         Permission = "dns:mutate"
	PermissionManageProviders   Permission = "providers:manage"
	PermissionManageUsers       Permission = "users:manage"
	PermissionReadAudit         Permission = "audit:read"
	PermissionManageOwnSecurity Permission = "security:self"
)

func (r Role) Valid() bool {
	switch r {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	default:
		return false
	}
}

func (r Role) Allows(permission Permission) bool {
	if !r.Valid() {
		return false
	}
	switch permission {
	case PermissionReadDNS, PermissionManageOwnSecurity:
		return true
	case PermissionReadAudit:
		return r == RoleAdmin || r == RoleOperator || r == RoleViewer
	case PermissionMutateDNS:
		return r == RoleAdmin || r == RoleOperator
	case PermissionManageProviders, PermissionManageUsers:
		return r == RoleAdmin
	default:
		return false
	}
}

// CanReadAuditEvent is intentionally an allowlist: non-admins can inspect
// only DNS changes, never authentication, user, credential, or security data.
func (r Role) CanReadAuditEvent(action, resourceType string) bool {
	if r == RoleAdmin {
		return true
	}
	if r != RoleOperator && r != RoleViewer {
		return false
	}
	return (resourceType == "zone" || resourceType == "recordset") &&
		(strings.HasPrefix(action, "zone.") || strings.HasPrefix(action, "recordset."))
}
