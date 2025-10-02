package security

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// GRPCInterceptor gRPC认证和限流拦截器
type GRPCInterceptor struct {
	authManager *AuthManager
	// 不需要认证的方法列表
	skipAuthMethods []string
	
	// 限流相关
	rateLimitEnabled   bool
	requestsPerMinute  int
	rateLimitClients   map[string]*grpcClientData
	rateLimitMutex     sync.RWMutex
}

// grpcClientData gRPC客户端限流数据
type grpcClientData struct {
	requests []time.Time
	mutex    sync.RWMutex
}

// NewGRPCInterceptor 创建gRPC认证和限流拦截器
func NewGRPCInterceptor(authManager *AuthManager) *GRPCInterceptor {
	interceptor := &GRPCInterceptor{
		authManager: authManager,
		skipAuthMethods: []string{
			"/olap.v1.OlapService/HealthCheck", // 健康检查不需要认证
		},
		rateLimitEnabled:  false,
		requestsPerMinute: 0,
		rateLimitClients:  make(map[string]*grpcClientData),
	}
	
	// 启动清理goroutine
	go interceptor.startRateLimitCleanup()
	
	return interceptor
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

// EnableRateLimit 启用gRPC限流
func (gi *GRPCInterceptor) EnableRateLimit(requestsPerMinute int) {
	gi.rateLimitMutex.Lock()
	defer gi.rateLimitMutex.Unlock()
	
	gi.rateLimitEnabled = requestsPerMinute > 0
	gi.requestsPerMinute = requestsPerMinute
}

// DisableRateLimit 禁用gRPC限流
func (gi *GRPCInterceptor) DisableRateLimit() {
	gi.rateLimitMutex.Lock()
	defer gi.rateLimitMutex.Unlock()
	
	gi.rateLimitEnabled = false
	gi.requestsPerMinute = 0
}

// RateLimitInterceptor gRPC限流拦截器
func (gi *GRPCInterceptor) RateLimitInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 检查是否启用限流
		gi.rateLimitMutex.RLock()
		enabled := gi.rateLimitEnabled
		limit := gi.requestsPerMinute
		gi.rateLimitMutex.RUnlock()
		
		if !enabled || limit <= 0 {
			return handler(ctx, req)
		}
		
		// 获取客户端IP
		clientIP := gi.getClientIP(ctx)
		if clientIP == "" {
			clientIP = "unknown"
		}
		
		// 检查限流
		if err := gi.checkRateLimit(clientIP, limit); err != nil {
			return nil, err
		}
		
		// 继续处理请求
		return handler(ctx, req)
	}
}

// StreamRateLimitInterceptor gRPC流式限流拦截器
func (gi *GRPCInterceptor) StreamRateLimitInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// 检查是否启用限流
		gi.rateLimitMutex.RLock()
		enabled := gi.rateLimitEnabled
		limit := gi.requestsPerMinute
		gi.rateLimitMutex.RUnlock()
		
		if !enabled || limit <= 0 {
			return handler(srv, stream)
		}
		
		// 获取客户端IP
		clientIP := gi.getClientIP(stream.Context())
		if clientIP == "" {
			clientIP = "unknown"
		}
		
		// 检查限流
		if err := gi.checkRateLimit(clientIP, limit); err != nil {
			return err
		}
		
		// 继续处理请求
		return handler(srv, stream)
	}
}

// getClientIP 从gRPC context中获取客户端IP
func (gi *GRPCInterceptor) getClientIP(ctx context.Context) string {
	// 尝试从peer信息获取
	if peer, ok := peer.FromContext(ctx); ok {
		if tcpAddr, ok := peer.Addr.(*net.TCPAddr); ok {
			return tcpAddr.IP.String()
		}
		// 如果不是TCP地址，返回地址字符串
		return peer.Addr.String()
	}
	
	// 尝试从metadata获取转发的IP
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		// 检查常见的转发头
		if xff := md.Get("x-forwarded-for"); len(xff) > 0 {
			return xff[0]
		}
		if xri := md.Get("x-real-ip"); len(xri) > 0 {
			return xri[0]
		}
	}
	
	return ""
}

// checkRateLimit 检查并记录请求的限流状态
func (gi *GRPCInterceptor) checkRateLimit(clientIP string, limit int) error {
	now := time.Now()
	
	// 获取或创建客户端数据
	gi.rateLimitMutex.RLock()
	data, exists := gi.rateLimitClients[clientIP]
	gi.rateLimitMutex.RUnlock()
	
	if !exists {
		gi.rateLimitMutex.Lock()
		// 双重检查
		if data, exists = gi.rateLimitClients[clientIP]; !exists {
			data = &grpcClientData{
				requests: make([]time.Time, 0, limit+10),
			}
			gi.rateLimitClients[clientIP] = data
		}
		gi.rateLimitMutex.Unlock()
	}
	
	data.mutex.Lock()
	defer data.mutex.Unlock()
	
	// 清理过期的请求记录
	var validRequests []time.Time
	for _, reqTime := range data.requests {
		if now.Sub(reqTime) < time.Minute {
			validRequests = append(validRequests, reqTime)
		}
	}
	data.requests = validRequests
	
	// 检查请求频率
	if len(data.requests) >= limit {
		return status.Errorf(codes.ResourceExhausted,
			"rate limit exceeded: %d requests per minute (client: %s)",
			limit, clientIP)
	}
	
	// 记录当前请求
	data.requests = append(data.requests, now)
	
	return nil
}

// startRateLimitCleanup 启动限流数据清理goroutine
func (gi *GRPCInterceptor) startRateLimitCleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			gi.cleanupRateLimitData()
		}
	}
}

// cleanupRateLimitData 清理过期的限流数据
func (gi *GRPCInterceptor) cleanupRateLimitData() {
	gi.rateLimitMutex.Lock()
	defer gi.rateLimitMutex.Unlock()
	
	now := time.Now()
	for ip, data := range gi.rateLimitClients {
		data.mutex.Lock()
		var validRequests []time.Time
		for _, reqTime := range data.requests {
			if now.Sub(reqTime) < time.Minute {
				validRequests = append(validRequests, reqTime)
			}
		}
		
		if len(validRequests) == 0 {
			delete(gi.rateLimitClients, ip)
		} else {
			data.requests = validRequests
		}
		data.mutex.Unlock()
	}
} 