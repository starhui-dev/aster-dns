package auth

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
	case PermissionReadDNS, PermissionReadAudit, PermissionManageOwnSecurity:
		return true
	case PermissionMutateDNS:
		return r == RoleAdmin || r == RoleOperator
	case PermissionManageProviders, PermissionManageUsers:
		return r == RoleAdmin
	default:
		return false
	}
}
