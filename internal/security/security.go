package security

import (
	"context"
	"fmt"
)

// 辅助函数

// IsPermissionValid 检查权限是否有效
func IsPermissionValid(perm Permission) bool {
	switch perm {
	case PermissionRead, PermissionWrite, PermissionDelete, PermissionAdmin:
		return true
	default:
		return false
	}
}

// IsPermissionListValid 检查权限列表是否有效
func IsPermissionListValid(perms []Permission) bool {
	for _, perm := range perms {
		if !IsPermissionValid(perm) {
			return false
		}
	}
	return len(perms) > 0
}

// IsRoleValid 检查角色是否有效
func IsRoleValid(role string) bool {
	// 默认支持的角色
	validRoles := []string{"admin", "analyst", "viewer", "editor", "user"}
	for _, validRole := range validRoles {
		if role == validRole {
			return true
		}
	}
	// 也允许自定义角色，只要不为空
	return role != ""
}

// IsRoleListValid 检查角色列表是否有效
func IsRoleListValid(roles []string) bool {
	for _, role := range roles {
		if !IsRoleValid(role) {
			return false
		}
	}
	return true
}

// HasPermission 检查权限列表中是否包含指定权限（从table_acl.go移动过来的通用版本）
func HasPermission(perms []Permission, required Permission) bool {
	// admin权限包含所有权限
	for _, p := range perms {
		if p == PermissionAdmin || p == required {
			return true
		}
	}
	return false
}

// CombinePermissions 合并权限列表，去除重复
func CombinePermissions(perms1, perms2 []Permission) []Permission {
	permMap := make(map[Permission]bool)

	// 添加perms1的所有权限
	for _, perm := range perms1 {
		permMap[perm] = true
	}

	// 添加perms2的所有权限
	for _, perm := range perms2 {
		permMap[perm] = true
	}

	// 转换回切片
	result := make([]Permission, 0, len(permMap))
	for perm := range permMap {
		result = append(result, perm)
	}

	return result
}

// SecurityError 安全相关的错误类型
type SecurityError struct {
	Code    string
	Message string
	Details interface{}
}

func (e *SecurityError) Error() string {
	return fmt.Sprintf("security error [%s]: %s", e.Code, e.Message)
}

// NewSecurityError 创建安全错误
func NewSecurityError(code, message string) *SecurityError {
	return &SecurityError{
		Code:    code,
		Message: message,
	}
}

// Common security errors
var (
	ErrInvalidToken       = NewSecurityError("INVALID_TOKEN", "Token is invalid")
	ErrExpiredToken       = NewSecurityError("EXPIRED_TOKEN", "Token has expired")
	ErrInsufficientRights = NewSecurityError("INSUFFICIENT_RIGHTS", "Insufficient rights for this operation")
	ErrSecurityDisabled   = NewSecurityError("SECURITY_DISABLED", "Security is disabled")
	ErrMissingAuthHeader  = NewSecurityError("MISSING_AUTH_HEADER", "Missing authorization header")
	ErrMalformedToken     = NewSecurityError("MALFORMED_TOKEN", "Token is malformed")
)

// PermissionScenario 权限场景
type PermissionScenario struct {
	UserID   string
	Table    string
	Action   Permission
	Roles    []string
	Expected bool
}

// RunPermissionScenarios 执行多个权限场景测试
func RunPermissionScenarios(aclManager *TableACLManager, scenarios []PermissionScenario) []error {
	errors := make([]error, 0)

	for _, scenario := range scenarios {
		result := aclManager.CheckPermission(scenario.Table, scenario.UserID, scenario.Action, scenario.Roles)

		if result != scenario.Expected {
			msg := fmt.Sprintf("Permission check failed for user %s on table %s with action %s: expected %v, got %v",
				scenario.UserID, scenario.Table, scenario.Action, scenario.Expected, result)
			errors = append(errors, fmt.Errorf("%s", msg))
		}
	}

	return errors
}

// ValidateUsername 验证用户名格式
func ValidateUsername(username string) bool {
	if len(username) < 3 || len(username) > 50 {
		return false
	}

	// 只允许字母、数字、下划线和短横线
	for _, ch := range username {
		if !((ch >= 'a' && ch <= 'z') ||
			 (ch >= 'A' && ch <= 'Z') ||
			 (ch >= '0' && ch <= '9') ||
			 ch == '_' || ch == '-') {
			return false
		}
	}

	return true
}

// SanitizeToken 清洁化token，移除潜在的危险字符
func SanitizeToken(token string) string {
	// 基本的token清洁化，移除可能导致问题的字符
	result := ""
	for _, ch := range token {
		if (ch >= 'a' && ch <= 'z') ||
		   (ch >= 'A' && ch <= 'Z') ||
		   (ch >= '0' && ch <= '9') ||
		   ch == '-' || ch == '_' || ch == '.' {
			result += string(ch)
		}
	}
	return result
}

// CreateTestContext 创建测试用的上下文
func CreateTestContext(userID, username string, roles []string) context.Context {
	user := &User{
		ID:       userID,
		Username: username,
		Roles:    roles,
	}

	return context.WithValue(context.Background(), UserContextKey, user)
}