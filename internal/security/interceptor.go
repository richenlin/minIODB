package security

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCInterceptor gRPC认证拦截器
type GRPCInterceptor struct {
	authManager *AuthManager
	// 不需要认证的方法列表
	skipAuthMethods []string
}

// NewGRPCInterceptor 创建gRPC认证拦截器
func NewGRPCInterceptor(authManager *AuthManager) *GRPCInterceptor {
	return &GRPCInterceptor{
		authManager: authManager,
		skipAuthMethods: []string{
			"/olap.v1.OlapService/HealthCheck", // 健康检查不需要认证
		},
	}
}

// SetSkipAuthMethods 设置不需要认证的方法列表
func (gi *GRPCInterceptor) SetSkipAuthMethods(methods []string) {
	gi.skipAuthMethods = methods
}

// UnaryServerInterceptor 一元RPC拦截器
func (gi *GRPCInterceptor) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 检查是否需要跳过认证
		if gi.shouldSkipAuth(info.FullMethod) {
			return handler(ctx, req)
		}

		// 如果认证被禁用，直接通过
		if !gi.authManager.IsEnabled() {
			return handler(ctx, req)
		}

		// 从metadata中提取token
		token, err := gi.extractTokenFromMetadata(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "missing or invalid token: %v", err)
		}

		// 验证token
		claims, err := gi.authManager.ValidateToken(token)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}

		// 将用户信息添加到context
		user := &User{
			ID:       claims.UserID,
			Username: claims.Username,
		}
		
		ctx = ContextWithUser(ctx, user)
		ctx = ContextWithClaims(ctx, claims)

		// 调用实际的处理函数
		return handler(ctx, req)
	}
}

// StreamServerInterceptor 流式RPC拦截器
func (gi *GRPCInterceptor) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// 检查是否需要跳过认证
		if gi.shouldSkipAuth(info.FullMethod) {
			return handler(srv, stream)
		}

		// 如果认证被禁用，直接通过
		if !gi.authManager.IsEnabled() {
			return handler(srv, stream)
		}

		// 从metadata中提取token
		token, err := gi.extractTokenFromMetadata(stream.Context())
		if err != nil {
			return status.Errorf(codes.Unauthenticated, "missing or invalid token: %v", err)
		}

		// 验证token
		claims, err := gi.authManager.ValidateToken(token)
		if err != nil {
			return status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}

		// 将用户信息添加到context
		user := &User{
			ID:       claims.UserID,
			Username: claims.Username,
		}
		
		ctx := ContextWithUser(stream.Context(), user)
		ctx = ContextWithClaims(ctx, claims)

		// 创建包装的stream
		wrappedStream := &wrappedServerStream{
			ServerStream: stream,
			ctx:          ctx,
		}

		// 调用实际的处理函数
		return handler(srv, wrappedStream)
	}
}

// shouldSkipAuth 检查是否应该跳过认证
func (gi *GRPCInterceptor) shouldSkipAuth(method string) bool {
	for _, skipMethod := range gi.skipAuthMethods {
		if method == skipMethod {
			return true
		}
	}
	return false
}

// extractTokenFromMetadata 从gRPC metadata中提取token
func (gi *GRPCInterceptor) extractTokenFromMetadata(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	// 尝试从Authorization header提取
	authHeaders := md.Get("authorization")
	if len(authHeaders) > 0 {
		authHeader := authHeaders[0]
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer "), nil
		}
		// 直接返回token（兼容不带Bearer前缀的情况）
		return authHeader, nil
	}

	// 尝试从token字段提取
	tokenHeaders := md.Get("token")
	if len(tokenHeaders) > 0 {
		return tokenHeaders[0], nil
	}

	return "", status.Error(codes.Unauthenticated, "missing token in metadata")
}

// wrappedServerStream 包装的服务器流，用于注入新的context
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context 返回包装的context
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

// RequireAuthMethod 要求认证的方法装饰器
func (gi *GRPCInterceptor) RequireAuthMethod(method string) {
	// 从跳过列表中移除该方法（如果存在）
	for i, skipMethod := range gi.skipAuthMethods {
		if skipMethod == method {
			gi.skipAuthMethods = append(gi.skipAuthMethods[:i], gi.skipAuthMethods[i+1:]...)
			break
		}
	}
}

// SkipAuthMethod 跳过认证的方法装饰器
func (gi *GRPCInterceptor) SkipAuthMethod(method string) {
	// 检查是否已经存在
	for _, skipMethod := range gi.skipAuthMethods {
		if skipMethod == method {
			return
		}
	}
	gi.skipAuthMethods = append(gi.skipAuthMethods, method)
} 