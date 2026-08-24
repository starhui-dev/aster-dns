package auth

import "testing"

func TestRolePermissionMatrix(t *testing.T) {
	tests := []struct {
		role       Role
		permission Permission
		allowed    bool
	}{
		{RoleViewer, PermissionReadDNS, true},
		{RoleViewer, PermissionMutateDNS, false},
		{RoleViewer, PermissionManageProviders, false},
		{RoleOperator, PermissionReadDNS, true},
		{RoleOperator, PermissionMutateDNS, true},
		{RoleOperator, PermissionManageProviders, false},
		{RoleOperator, PermissionManageUsers, false},
		{RoleAdmin, PermissionReadDNS, true},
		{RoleAdmin, PermissionMutateDNS, true},
		{RoleAdmin, PermissionManageProviders, true},
		{RoleAdmin, PermissionManageUsers, true},
	}
	for _, test := range tests {
		if got := test.role.Allows(test.permission); got != test.allowed {
			t.Errorf("role %q permission %q = %v, want %v", test.role, test.permission, got, test.allowed)
		}
	}
}
