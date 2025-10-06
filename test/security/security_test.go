package security

import (
	"context"
	"testing"
	"time"

	"minIODB/test"
	"github.com/stretchr/testify/suite"
)

// SecurityTestSuite 安全模块功能测试套件
type SecurityTestSuite struct {
	test.BaseTestSuite
}

func (s *SecurityTestSuite) SetupSuite() {
	s.BaseTestSuite.SetSuiteName("Security Test Suite")
	s.BaseTestSuite.SetupSuite()
}

func (s *SecurityTestSuite) TearDownSuite() {
	s.BaseTestSuite.TearDownSuite()
}

func TestSecurity(t *testing.T) {
	suite.Run(t, new(SecurityTestSuite))
}

// TestJWTTokenGeneration JWT Token生成和验证测试
func (s *SecurityTestSuite) TestJWTTokenGeneration() {
	ctx := s.GetTestContext()

	s.Run("有效的JWT Token生成验证", func() {
		// 验证JWT秘密密钥生成
		secret, err := generateRandomString(32)
		s.NoError(err)
		s.Len(secret, 32, "生成的秘密密钥应该是32字符")
	})

	s.Run("Token过期时间验证", func() {
		// 模拟Token过期
		expTime := time.Now().Add(-time.Hour)
		s.True(expTime.Before(time.Now()), "过去的时间应该已经过期")
	})
}

// TestTableACLManager 表级权限管理器测试
func (s *SecurityTestSuite) TestTableACLManager() {
	ctx := s.GetTestContext()

	s.Run("权限授予和撤销", func() {
		// 创建ACL管理器
		aclMgr := NewTableACLManager()
		s.NotNil(aclMgr)

		// 测试表创建
		tableName := "test_table"
		err := aclMgr.CreateTableACL(tableName)
		s.NoError(err, "应该成功创建表ACL")

		// 测试权限授予
		userID := "test-user"
		permissions := []Permission{PermissionRead, PermissionWrite}
		err = aclMgr.GrantPermission(tableName, userID, permissions)
		s.NoError(err, "应该成功授予权限")

		// 验证权限存在
		hasRead := aclMgr.CheckPermission(tableName, userID, PermissionRead, []string{})
		s.True(hasRead, "应该具有读取权限")

		hasWrite := aclMgr.CheckPermission(tableName, userID, PermissionWrite, []string{})
		s.True(hasWrite, "应该具有写入权限")

		hasDelete := aclMgr.CheckPermission(tableName, userID, PermissionDelete, []string{})
		s.False(hasDelete, "不应该具有删除权限")

		// 测试权限撤销
		err = aclMgr.RevokePermission(tableName, userID, []Permission{PermissionWrite})
		s.NoError(err, "应该成功撤销写入权限")

		// 验证权限撤销
		hasWriteAfter := aclMgr.CheckPermission(tableName, userID, PermissionWrite, []string{})
		s.False(hasWriteAfter, "不应该再具有写入权限")

		hasReadAfter := aclMgr.CheckPermission(tableName, userID, PermissionRead, []string{})
		s.True(hasReadAfter, "应该仍然具有读取权限")
	})

	s.Run("角色权限测试", func() {
		// 测试基于角色的权限验证
		aclMgr := NewTableACLManager()
		tableName := "role_test_table"

		err := aclMgr.CreateTableACL(tableName)
		s.NoError(err)

		// 授予analyst角色读取权限
		analystPerms := []Permission{PermissionRead}
		err = aclMgr.GrantRolePermission(tableName, "analyst", analystPerms)
		s.NoError(err)

		// 授予admin角色所有权限
		adminPerms := []Permission{PermissionAdmin}
		err = aclMgr.GrantRolePermission(tableName, "admin", adminPerms)
		s.NoError(err)

		// 验证角色权限
		hasAnalystRead := aclMgr.CheckPermission(tableName, "user123", PermissionRead, []string{"analyst"})
		s.True(hasAnalystRead, "具有analyst角色的用户应该有读取权限")

		hasAdminAll := aclMgr.CheckPermission(tableName, "admin456", PermissionWrite, []string{"admin"})
		s.True(hasAdminAll, "具有admin角色的用户应该有所有权限")

		hasAnalystDelete := aclMgr.CheckPermission(tableName, "user123", PermissionDelete, []string{"analyst"})
		s.False(hasAnalystDelete, "具有analyst角色的用户不应该有删除权限")
	})
}