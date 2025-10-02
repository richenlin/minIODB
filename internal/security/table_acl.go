package security

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Permission 权限类型
type Permission string

const (
	PermissionRead   Permission = "read"
	PermissionWrite  Permission = "write"
	PermissionDelete Permission = "delete"
	PermissionAdmin  Permission = "admin" // 包含所有权限
)

// TableACL 表级访问控制列表
type TableACL struct {
	TableName string                  `json:"table_name"`
	UserPerms map[string][]Permission `json:"user_perms"` // 用户ID -> 权限列表
	RolePerms map[string][]Permission `json:"role_perms"` // 角色 -> 权限列表
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
	mutex     sync.RWMutex
}

// TableACLManager 表级权限管理器
type TableACLManager struct {
	acls  map[string]*TableACL // tableName -> ACL
	mutex sync.RWMutex
}

// NewTableACLManager 创建表级权限管理器
func NewTableACLManager() *TableACLManager {
	return &TableACLManager{
		acls: make(map[string]*TableACL),
	}
}

// CreateTableACL 为表创建ACL
func (tam *TableACLManager) CreateTableACL(tableName string) error {
	tam.mutex.Lock()
	defer tam.mutex.Unlock()

	if _, exists := tam.acls[tableName]; exists {
		return fmt.Errorf("ACL for table %s already exists", tableName)
	}

	tam.acls[tableName] = &TableACL{
		TableName: tableName,
		UserPerms: make(map[string][]Permission),
		RolePerms: make(map[string][]Permission),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return nil
}

// GrantPermission 授予用户对表的权限
func (tam *TableACLManager) GrantPermission(tableName, userID string, perms []Permission) error {
	tam.mutex.RLock()
	acl, exists := tam.acls[tableName]
	tam.mutex.RUnlock()

	if !exists {
		// 自动创建ACL
		if err := tam.CreateTableACL(tableName); err != nil {
			return err
		}
		tam.mutex.RLock()
		acl = tam.acls[tableName]
		tam.mutex.RUnlock()
	}

	acl.mutex.Lock()
	defer acl.mutex.Unlock()

	// 合并权限（去重）
	existingPerms := acl.UserPerms[userID]
	permMap := make(map[Permission]bool)
	for _, p := range existingPerms {
		permMap[p] = true
	}
	for _, p := range perms {
		permMap[p] = true
	}

	newPerms := make([]Permission, 0, len(permMap))
	for p := range permMap {
		newPerms = append(newPerms, p)
	}

	acl.UserPerms[userID] = newPerms
	acl.UpdatedAt = time.Now()

	return nil
}

// RevokePermission 撤销用户对表的权限
func (tam *TableACLManager) RevokePermission(tableName, userID string, perms []Permission) error {
	tam.mutex.RLock()
	acl, exists := tam.acls[tableName]
	tam.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("ACL for table %s not found", tableName)
	}

	acl.mutex.Lock()
	defer acl.mutex.Unlock()

	existingPerms := acl.UserPerms[userID]
	if len(existingPerms) == 0 {
		return nil // 用户本来就没有权限
	}

	// 创建要撤销的权限map
	revokeMap := make(map[Permission]bool)
	for _, p := range perms {
		revokeMap[p] = true
	}

	// 过滤掉要撤销的权限
	newPerms := make([]Permission, 0)
	for _, p := range existingPerms {
		if !revokeMap[p] {
			newPerms = append(newPerms, p)
		}
	}

	if len(newPerms) == 0 {
		delete(acl.UserPerms, userID)
	} else {
		acl.UserPerms[userID] = newPerms
	}
	acl.UpdatedAt = time.Now()

	return nil
}

// GrantRolePermission 授予角色对表的权限
func (tam *TableACLManager) GrantRolePermission(tableName, role string, perms []Permission) error {
	tam.mutex.RLock()
	acl, exists := tam.acls[tableName]
	tam.mutex.RUnlock()

	if !exists {
		if err := tam.CreateTableACL(tableName); err != nil {
			return err
		}
		tam.mutex.RLock()
		acl = tam.acls[tableName]
		tam.mutex.RUnlock()
	}

	acl.mutex.Lock()
	defer acl.mutex.Unlock()

	// 合并权限
	existingPerms := acl.RolePerms[role]
	permMap := make(map[Permission]bool)
	for _, p := range existingPerms {
		permMap[p] = true
	}
	for _, p := range perms {
		permMap[p] = true
	}

	newPerms := make([]Permission, 0, len(permMap))
	for p := range permMap {
		newPerms = append(newPerms, p)
	}

	acl.RolePerms[role] = newPerms
	acl.UpdatedAt = time.Now()

	return nil
}

// CheckPermission 检查用户是否有对表的特定权限
func (tam *TableACLManager) CheckPermission(tableName, userID string, requiredPerm Permission, userRoles []string) bool {
	tam.mutex.RLock()
	acl, exists := tam.acls[tableName]
	tam.mutex.RUnlock()

	if !exists {
		// 如果表没有ACL，默认允许所有操作
		return true
	}

	acl.mutex.RLock()
	defer acl.mutex.RUnlock()

	// 1. 检查用户直接权限
	userPerms := acl.UserPerms[userID]
	if hasPermission(userPerms, requiredPerm) {
		return true
	}

	// 2. 检查用户的角色权限
	for _, role := range userRoles {
		rolePerms := acl.RolePerms[role]
		if hasPermission(rolePerms, requiredPerm) {
			return true
		}
	}

	return false
}

// hasPermission 检查权限列表中是否包含所需权限
func hasPermission(perms []Permission, required Permission) bool {
	// admin权限包含所有权限
	for _, p := range perms {
		if p == PermissionAdmin || p == required {
			return true
		}
	}
	return false
}

// GetTableACL 获取表的ACL
func (tam *TableACLManager) GetTableACL(tableName string) (*TableACL, error) {
	tam.mutex.RLock()
	defer tam.mutex.RUnlock()

	acl, exists := tam.acls[tableName]
	if !exists {
		return nil, fmt.Errorf("ACL for table %s not found", tableName)
	}

	// 返回副本以避免并发问题
	acl.mutex.RLock()
	defer acl.mutex.RUnlock()

	aclCopy := &TableACL{
		TableName: acl.TableName,
		UserPerms: make(map[string][]Permission),
		RolePerms: make(map[string][]Permission),
		CreatedAt: acl.CreatedAt,
		UpdatedAt: acl.UpdatedAt,
	}

	for k, v := range acl.UserPerms {
		aclCopy.UserPerms[k] = append([]Permission{}, v...)
	}
	for k, v := range acl.RolePerms {
		aclCopy.RolePerms[k] = append([]Permission{}, v...)
	}

	return aclCopy, nil
}

// DeleteTableACL 删除表的ACL
func (tam *TableACLManager) DeleteTableACL(tableName string) error {
	tam.mutex.Lock()
	defer tam.mutex.Unlock()

	delete(tam.acls, tableName)
	return nil
}

// ListTableACLs 列出所有表的ACL
func (tam *TableACLManager) ListTableACLs() []string {
	tam.mutex.RLock()
	defer tam.mutex.RUnlock()

	tables := make([]string, 0, len(tam.acls))
	for tableName := range tam.acls {
		tables = append(tables, tableName)
	}
	return tables
}

// GetUserContextKey 从上下文获取用户ID
func GetUserIDFromContext(ctx context.Context) string {
	if user, ok := ctx.Value(UserContextKey).(*User); ok {
		return user.ID
	}
	return ""
}

// GetUserRolesFromContext 从上下文获取用户角色
func GetUserRolesFromContext(ctx context.Context) []string {
	if claims, ok := ctx.Value(ClaimsContextKey).(*Claims); ok {
		// 可以从claims中获取角色信息
		// 这里简化实现，返回默认角色
		if claims.Username == "admin" {
			return []string{"admin"}
		}
		return []string{"user"}
	}
	return []string{}
}
