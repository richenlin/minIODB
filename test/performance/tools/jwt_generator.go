package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims JWT声明结构
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func main() {
	// 从环境变量或参数获取JWT密钥
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "your-super-secret-jwt-key-change-in-production"
	}

	// 检查命令行参数
	if len(os.Args) < 3 {
		fmt.Println("使用方法: jwt_generator <user_id> <username> [jwt_secret]")
		fmt.Println("示例: jwt_generator test-user test-user")
		fmt.Println("或者: JWT_SECRET=your-secret jwt_generator test-user test-user")
		os.Exit(1)
	}

	userID := os.Args[1]
	username := os.Args[2]

	// 如果提供了第三个参数，使用它作为JWT密钥
	if len(os.Args) > 3 {
		jwtSecret = os.Args[3]
	}

	// 创建JWT claims
	claims := &Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "miniodb",
			Audience:  []string{"miniodb-rest"},
		},
	}

	// 创建token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		log.Fatalf("生成JWT token失败: %v", err)
	}

	fmt.Printf("JWT Token: %s\n", tokenString)
	fmt.Printf("用户ID: %s\n", userID)
	fmt.Printf("用户名: %s\n", username)
	fmt.Printf("过期时间: %s\n", time.Now().Add(24*time.Hour).Format("2006-01-02 15:04:05"))
	fmt.Printf("\n使用示例:\n")
	fmt.Printf("curl -H \"Authorization: Bearer %s\" http://localhost:8081/v1/health\n", tokenString)
}
