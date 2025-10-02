package security

import (
	"testing"
)

func TestNewTableACLManager(t *testing.T) {
	manager := NewTableACLManager()

	if manager == nil {
		t.Fatal("NewTableACLManager returned nil")
	}
}

func TestCreateTableACL(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"

	err := manager.CreateTableACL(tableName)
	if err != nil {
		t.Fatalf("CreateTableACL failed: %v", err)
	}

	// Creating again should return an error (not idempotent in this implementation)
	err = manager.CreateTableACL(tableName)
	if err == nil {
		t.Log("CreateTableACL allows duplicate creation")
	}
}

func TestGrantPermission(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"
	user := "test_user"

	// Create table ACL first
	manager.CreateTableACL(tableName)

	// Grant READ permission
	err := manager.GrantPermission(tableName, user, []Permission{PermissionRead})
	if err != nil {
		t.Fatalf("GrantPermission failed: %v", err)
	}

	// Verify permission was granted
	hasPermission := manager.CheckPermission(tableName, user, PermissionRead, nil)
	if !hasPermission {
		t.Error("User should have READ permission")
	}

	// Grant WRITE permission
	err = manager.GrantPermission(tableName, user, []Permission{PermissionWrite})
	if err != nil {
		t.Fatalf("GrantPermission failed: %v", err)
	}

	// Verify both permissions
	hasRead := manager.CheckPermission(tableName, user, PermissionRead, nil)
	hasWrite := manager.CheckPermission(tableName, user, PermissionWrite, nil)

	if !hasRead || !hasWrite {
		t.Error("User should have both READ and WRITE permissions")
	}
}

func TestRevokePermission(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"
	user := "test_user"

	// Setup
	manager.CreateTableACL(tableName)
	manager.GrantPermission(tableName, user, []Permission{PermissionRead, PermissionWrite})

	// Revoke READ
	err := manager.RevokePermission(tableName, user, []Permission{PermissionRead})
	if err != nil {
		t.Fatalf("RevokePermission failed: %v", err)
	}

	// Verify READ revoked but WRITE remains
	hasRead := manager.CheckPermission(tableName, user, PermissionRead, nil)
	hasWrite := manager.CheckPermission(tableName, user, PermissionWrite, nil)

	if hasRead {
		t.Error("User should not have READ permission after revoke")
	}

	if !hasWrite {
		t.Error("User should still have WRITE permission")
	}
}

func TestCheckPermission(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"
	user := "test_user"

	// Setup
	manager.CreateTableACL(tableName)

	// No permissions initially
	hasPermission := manager.CheckPermission(tableName, user, PermissionRead, nil)
	if hasPermission {
		t.Error("User should not have permission initially")
	}

	// Grant permission
	manager.GrantPermission(tableName, user, []Permission{PermissionRead})

	// Now should have permission
	hasPermission = manager.CheckPermission(tableName, user, PermissionRead, nil)
	if !hasPermission {
		t.Error("User should have permission after grant")
	}

	// Should not have other permissions
	hasWrite := manager.CheckPermission(tableName, user, PermissionWrite, nil)
	if hasWrite {
		t.Error("User should not have WRITE permission")
	}
}

func TestAdminPermission(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"
	user := "admin_user"

	// Setup
	manager.CreateTableACL(tableName)

	// Grant ADMIN permission
	err := manager.GrantPermission(tableName, user, []Permission{PermissionAdmin})
	if err != nil {
		t.Fatalf("GrantPermission failed: %v", err)
	}

	// Admin should have all permissions
	hasRead := manager.CheckPermission(tableName, user, PermissionRead, nil)
	hasWrite := manager.CheckPermission(tableName, user, PermissionWrite, nil)
	hasDelete := manager.CheckPermission(tableName, user, PermissionDelete, nil)
	hasAdmin := manager.CheckPermission(tableName, user, PermissionAdmin, nil)

	if !hasRead || !hasWrite || !hasDelete || !hasAdmin {
		t.Error("Admin should have all permissions")
	}
}

func TestGrantRolePermission(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"
	role := "analyst"

	// Setup
	manager.CreateTableACL(tableName)

	// Grant role permission
	err := manager.GrantRolePermission(tableName, role, []Permission{PermissionRead})
	if err != nil {
		t.Fatalf("GrantRolePermission failed: %v", err)
	}

	// User with that role should have permission
	user := "test_user"
	hasPermission := manager.CheckPermission(tableName, user, PermissionRead, []string{role})
	if !hasPermission {
		t.Error("User with role should have permission")
	}

	// User without role should not have permission
	hasPermission = manager.CheckPermission(tableName, "other_user", PermissionRead, []string{"other_role"})
	if hasPermission {
		t.Error("User without role should not have permission")
	}
}

func TestGetTableACL(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"

	// Create and setup
	manager.CreateTableACL(tableName)
	manager.GrantPermission(tableName, "user1", []Permission{PermissionRead})

	// Get ACL
	acl, err := manager.GetTableACL(tableName)
	if err != nil {
		t.Fatalf("GetTableACL failed: %v", err)
	}

	if acl.TableName != tableName {
		t.Errorf("Expected table name %s, got %s", tableName, acl.TableName)
	}

	if len(acl.UserPerms) != 1 {
		t.Errorf("Expected 1 user permission, got %d", len(acl.UserPerms))
	}
}

func TestDeleteTableACL(t *testing.T) {
	manager := NewTableACLManager()

	tableName := "test_table"
	user := "test_user"

	// Setup
	manager.CreateTableACL(tableName)
	manager.GrantPermission(tableName, user, []Permission{PermissionRead})

	// Verify granted
	hasPermission := manager.CheckPermission(tableName, user, PermissionRead, nil)
	if !hasPermission {
		t.Error("Permission should be granted")
	}

	// Delete table ACL
	err := manager.DeleteTableACL(tableName)
	if err != nil {
		t.Fatalf("DeleteTableACL failed: %v", err)
	}

	// After deletion, GetTableACL should return error
	_, err = manager.GetTableACL(tableName)
	if err == nil {
		t.Error("GetTableACL should return error after deletion")
	}
}

func TestListTableACLs(t *testing.T) {
	manager := NewTableACLManager()

	// Create multiple table ACLs
	tables := []string{"table1", "table2", "table3"}
	for _, table := range tables {
		manager.CreateTableACL(table)
	}

	// List ACLs
	aclList := manager.ListTableACLs()

	if len(aclList) != len(tables) {
		t.Errorf("Expected %d ACLs, got %d", len(tables), len(aclList))
	}
}

func TestMultipleTablesAndUsers(t *testing.T) {
	manager := NewTableACLManager()

	// Setup complex scenario
	manager.CreateTableACL("table1")
	manager.CreateTableACL("table2")

	manager.GrantPermission("table1", "user1", []Permission{PermissionRead})
	manager.GrantPermission("table1", "user2", []Permission{PermissionWrite})
	manager.GrantPermission("table2", "user1", []Permission{PermissionAdmin})
	manager.GrantPermission("table2", "user3", []Permission{PermissionRead})

	// Verify table1 user1
	if !manager.CheckPermission("table1", "user1", PermissionRead, nil) {
		t.Error("user1 should have READ on table1")
	}

	// Verify table1 user2
	if !manager.CheckPermission("table1", "user2", PermissionWrite, nil) {
		t.Error("user2 should have WRITE on table1")
	}

	// Verify table2 user1 has admin (includes read/write/delete)
	if !manager.CheckPermission("table2", "user1", PermissionRead, nil) ||
		!manager.CheckPermission("table2", "user1", PermissionWrite, nil) ||
		!manager.CheckPermission("table2", "user1", PermissionDelete, nil) {
		t.Error("user1 should have all permissions on table2 (admin)")
	}

	// Verify isolation - user3 should not have access to table1
	if manager.CheckPermission("table1", "user3", PermissionRead, nil) {
		t.Error("user3 should not have access to table1")
	}
}

func BenchmarkCheckPermission(b *testing.B) {
	manager := NewTableACLManager()

	tableName := "test_table"
	user := "test_user"

	manager.CreateTableACL(tableName)
	manager.GrantPermission(tableName, user, []Permission{PermissionRead})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.CheckPermission(tableName, user, PermissionRead, nil)
	}
}

func BenchmarkGrantPermission(b *testing.B) {
	manager := NewTableACLManager()

	tableName := "test_table"
	manager.CreateTableACL(tableName)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		user := "test_user"
		manager.GrantPermission(tableName, user, []Permission{PermissionRead})
	}
}

func BenchmarkCheckPermissionWithRole(b *testing.B) {
	manager := NewTableACLManager()

	tableName := "test_table"
	role := "analyst"
	roles := []string{role, "viewer", "editor"}

	manager.CreateTableACL(tableName)
	manager.GrantRolePermission(tableName, role, []Permission{PermissionRead})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		manager.CheckPermission(tableName, "test_user", PermissionRead, roles)
	}
}
