package security

import (
	"context"
	"fmt"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// GRPCSmartRateLimiter gRPC智能限流拦截器
type GRPCSmartRateLimiter struct {
	smartRateLimiter *SmartRateLimiter
}

// NewGRPCSmartRateLimiter 创建gRPC智能限流拦截器
func NewGRPCSmartRateLimiter(smartRateLimiter *SmartRateLimiter) *GRPCSmartRateLimiter {
	return &GRPCSmartRateLimiter{
		smartRateLimiter: smartRateLimiter,
	}
}

// UnaryServerInterceptor 一元RPC智能限流拦截器
func (gsrl *GRPCSmartRateLimiter) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 获取客户端IP
		clientIP := gsrl.getClientIP(ctx)
		
		// 将gRPC方法路径转换为REST风格路径以便复用限流规则
		path := gsrl.grpcMethodToRESTPath(info.FullMethod)
		
		// 检查限流
		allowed, waitTime, err := gsrl.smartRateLimiter.checkRateLimit(clientIP, path)
		
		if !allowed {
			// 获取对应的限流等级信息
			tier := gsrl.smartRateLimiter.getTierForPath(path)
			
			// 返回gRPC状态错误
			return nil, status.Errorf(codes.ResourceExhausted, 
				"Rate limit exceeded for %s API. Limit: %.0f req/s, Burst: %d, Retry after: %.1fs. Details: %v",
				tier.Name, tier.RequestsPerSec, tier.BurstSize, waitTime.Seconds(), err)
		}
		
		// 限流通过，继续处理请求
		return handler(ctx, req)
	}
}

// StreamServerInterceptor 流式RPC智能限流拦截器
func (gsrl *GRPCSmartRateLimiter) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// 获取客户端IP
		clientIP := gsrl.getClientIP(stream.Context())
		
		// 将gRPC方法路径转换为REST风格路径以便复用限流规则
		path := gsrl.grpcMethodToRESTPath(info.FullMethod)
		
		// 检查限流
		allowed, waitTime, err := gsrl.smartRateLimiter.checkRateLimit(clientIP, path)
		
		if !allowed {
			// 获取对应的限流等级信息
			tier := gsrl.smartRateLimiter.getTierForPath(path)
			
			// 返回gRPC状态错误
			return status.Errorf(codes.ResourceExhausted, 
				"Rate limit exceeded for %s API. Limit: %.0f req/s, Burst: %d, Retry after: %.1fs. Details: %v",
				tier.Name, tier.RequestsPerSec, tier.BurstSize, waitTime.Seconds(), err)
		}
		
		// 限流通过，继续处理请求
		return handler(srv, stream)
	}
}

// getClientIP 获取客户端IP地址
func (gsrl *GRPCSmartRateLimiter) getClientIP(ctx context.Context) string {
	// 尝试从peer信息获取
	if p, ok := peer.FromContext(ctx); ok {
		if tcpAddr, ok := p.Addr.(*net.TCPAddr); ok {
			return tcpAddr.IP.String()
		}
		// 解析地址字符串
		addr := p.Addr.String()
		if host, _, err := net.SplitHostPort(addr); err == nil {
			return host
		}
		return addr
	}
	
	// 默认返回本地地址
	return "127.0.0.1"
}

// grpcMethodToRESTPath 将gRPC方法路径转换为REST风格路径
// 这样可以复用现有的路径限流配置
func (gsrl *GRPCSmartRateLimiter) grpcMethodToRESTPath(method string) string {
	// method格式: /olap.v1.OlapService/MethodName
	// 转换为: /v1/methodname
	
	// 移除包名和服务名，只保留方法名
	parts := strings.Split(method, "/")
	if len(parts) < 2 {
		return "/v1/unknown"
	}
	
	methodName := parts[len(parts)-1]
	
	// 根据方法名映射到对应的REST路径
	switch methodName {
	case "HealthCheck":
		return "/v1/health"
	case "Write":
		return "/v1/data"
	case "Query":
		return "/v1/query"
	case "TriggerBackup":
		return "/v1/backup"
	case "RecoverData":
		return "/v1/recover"
	case "GetStats":
		return "/v1/stats"
	case "GetNodes":
		return "/v1/nodes"
	case "TriggerMetadataBackup":
		return "/v1/metadata/backup"
	case "ListMetadataBackups":
		return "/v1/metadata/backups"
	case "RecoverMetadata":
		return "/v1/metadata/recover"
	case "GetMetadataStatus":
		return "/v1/metadata/status"
	case "ValidateMetadataBackup":
		return "/v1/metadata/validate"
	default:
		// 对于未知方法，转换为小写并添加前缀
		return fmt.Sprintf("/v1/%s", strings.ToLower(methodName))
	}
}

// GetStats 获取限流统计信息
func (gsrl *GRPCSmartRateLimiter) GetStats() map[string]interface{} {
	if gsrl.smartRateLimiter == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}
	
	stats := gsrl.smartRateLimiter.GetStats()
	stats["type"] = "grpc_smart_rate_limiter"
	return stats
} 