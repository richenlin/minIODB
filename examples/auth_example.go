package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"minIODB/internal/security"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// 演示如何使用认证功能
func main() {
	// 1. 创建认证管理器
	authConfig := &security.AuthConfig{
		Mode:            "token",
		JWTSecret:       "my-secret-key-for-demo",
		TokenExpiration: 24 * time.Hour,
		Issuer:          "miniodb-demo",
		Audience:        "miniodb-api",
		ValidTokens:     []string{"static-token-123", "admin-token-456"},
	}

	authManager, err := security.NewAuthManager(authConfig)
	if err != nil {
		log.Fatalf("Failed to create auth manager: %v", err)
	}

	// 2. 生成JWT Token
	token, err := authManager.GenerateToken("user123", "testuser")
	if err != nil {
		log.Fatalf("Failed to generate token: %v", err)
	}
	
	fmt.Printf("Generated JWT Token: %s\n", token)

	// 3. 验证Token
	claims, err := authManager.ValidateToken(token)
	if err != nil {
		log.Fatalf("Failed to validate token: %v", err)
	}
	
	fmt.Printf("Token validated successfully! User: %s (ID: %s)\n", claims.Username, claims.UserID)

	// 4. 测试静态Token
	staticClaims, err := authManager.ValidateToken("static-token-123")
	if err != nil {
		log.Fatalf("Failed to validate static token: %v", err)
	}
	
	fmt.Printf("Static token validated! User: %s (ID: %s)\n", staticClaims.Username, staticClaims.UserID)

	// 5. 演示gRPC客户端如何使用认证
	demonstrateGRPCAuth(token)

	// 6. 演示REST API如何使用认证
	demonstrateRESTAuth(token)
}

// 演示gRPC认证
func demonstrateGRPCAuth(token string) {
	fmt.Println("\n=== gRPC认证示例 ===")
	
	// 创建带认证的gRPC连接
	conn, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
	if err != nil {
		log.Printf("Failed to connect to gRPC server: %v", err)
		return
	}
	defer conn.Close()

	// 创建带认证信息的context
	ctx := context.Background()
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

	fmt.Printf("gRPC请求可以使用以下metadata进行认证:\n")
	fmt.Printf("  authorization: Bearer %s\n", token)
	fmt.Printf("  或者直接使用: token: %s\n", token)
}

// 演示REST API认证
func demonstrateRESTAuth(token string) {
	fmt.Println("\n=== REST API认证示例 ===")
	
	fmt.Printf("REST API请求可以通过以下方式携带认证信息:\n")
	fmt.Printf("1. Authorization Header:\n")
	fmt.Printf("   Authorization: Bearer %s\n", token)
	fmt.Printf("   或者直接: Authorization: %s\n", token)
	fmt.Printf("\n2. Query Parameter:\n")
	fmt.Printf("   GET /v1/data?token=%s\n", token)
	fmt.Printf("\n3. Form Data:\n")
	fmt.Printf("   POST /v1/data\n")
	fmt.Printf("   Content-Type: application/x-www-form-urlencoded\n")
	fmt.Printf("   token=%s&other_data=...\n", token)
	
	fmt.Printf("\n需要认证的端点:\n")
	fmt.Printf("  - POST /v1/data (数据写入)\n")
	fmt.Printf("  - POST /v1/query (数据查询)\n")
	fmt.Printf("  - POST /v1/backup/trigger (触发备份)\n")
	fmt.Printf("  - POST /v1/recover (数据恢复)\n")
	fmt.Printf("  - GET /v1/stats (系统状态)\n")
	fmt.Printf("  - GET /v1/nodes (节点信息)\n")
	
	fmt.Printf("\n不需要认证的端点:\n")
	fmt.Printf("  - GET /v1/health (健康检查)\n")
} 