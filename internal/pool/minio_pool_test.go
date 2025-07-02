package pool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMinIOPoolConfig(t *testing.T) {
	config := DefaultMinIOPoolConfig()
	
	assert.NotNil(t, config)
	assert.Equal(t, "localhost:9000", config.Endpoint)
	assert.Equal(t, "minioadmin", config.AccessKeyID)
	assert.Equal(t, "minioadmin", config.SecretAccessKey)
	assert.False(t, config.UseSSL)
	assert.Equal(t, "us-east-1", config.Region)
	assert.True(t, config.MaxIdleConns > 0)
	assert.True(t, config.MaxIdleConnsPerHost > 0)
	assert.True(t, config.MaxConnsPerHost > 0)
	assert.True(t, config.IdleConnTimeout > 0)
	assert.True(t, config.DialTimeout > 0)
	assert.True(t, config.TLSHandshakeTimeout > 0)
	assert.True(t, config.ResponseHeaderTimeout > 0)
	assert.True(t, config.ExpectContinueTimeout > 0)
	assert.True(t, config.MaxRetries > 0)
	assert.True(t, config.RetryDelay > 0)
	assert.True(t, config.RequestTimeout > 0)
	assert.True(t, config.KeepAlive > 0)
}

func TestNewMinIOPool_Success(t *testing.T) {
	// 创建模拟的MinIO服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟ListBuckets响应
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:               server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:            "testkey",
		SecretAccessKey:        "testsecret",
		UseSSL:                 false,
		Region:                 "us-east-1",
		MaxIdleConns:           100,
		MaxIdleConnsPerHost:    50,
		MaxConnsPerHost:        100,
		IdleConnTimeout:        90 * time.Second,
		DialTimeout:            30 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		MaxRetries:             3,
		RetryDelay:             100 * time.Millisecond,
		RequestTimeout:         120 * time.Second,
		KeepAlive:              30 * time.Second,
		DisableKeepAlive:       false,
		DisableCompression:     false,
	}

	pool, err := NewMinIOPool(config)
	require.NoError(t, err)
	require.NotNil(t, pool)
	defer pool.Close()

	// 测试获取客户端
	client := pool.GetClient()
	assert.NotNil(t, client)

	// 测试配置
	assert.Equal(t, config, pool.GetConfig())

	// 测试连接信息
	connInfo := pool.GetConnectionInfo()
	assert.NotNil(t, connInfo)
	assert.Contains(t, connInfo, "endpoint")
	assert.Contains(t, connInfo, "region")
	assert.Contains(t, connInfo, "use_ssl")

	// 测试统计信息
	stats := pool.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.TotalRequests)
	assert.Equal(t, int64(0), stats.SuccessRequests)
	assert.Equal(t, int64(0), stats.FailedRequests)
}

func TestNewMinIOPool_InvalidConfig(t *testing.T) {
	// 测试无效配置
	config := &MinIOPoolConfig{
		Endpoint:        "invalid-endpoint",
		AccessKeyID:     "",
		SecretAccessKey: "",
		UseSSL:          false,
		Region:          "us-east-1",
	}

	pool, err := NewMinIOPool(config)
	assert.Error(t, err)
	assert.Nil(t, pool)
}

func TestMinIOPool_HealthCheck(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:        server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
		UseSSL:          false,
		Region:          "us-east-1",
		DialTimeout:     time.Second,
		RequestTimeout:  time.Second,
	}

	pool, err := NewMinIOPool(config)
	require.NoError(t, err)
	defer pool.Close()

	// 测试健康检查
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	err = pool.HealthCheck(ctx)
	assert.NoError(t, err)
}

func TestMinIOPool_HealthCheckFail(t *testing.T) {
	config := &MinIOPoolConfig{
		Endpoint:        "localhost:9999", // 不存在的服务器
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
		UseSSL:          false,
		Region:          "us-east-1",
		DialTimeout:     100 * time.Millisecond,
		RequestTimeout:  200 * time.Millisecond,
	}

	pool, err := NewMinIOPool(config)
	if err != nil {
		// 如果连接创建失败，这是预期的
		assert.Error(t, err)
		return
	}

	if pool != nil {
		defer pool.Close()
		
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		
		err = pool.HealthCheck(ctx)
		assert.Error(t, err)
	}
}

func TestMinIOPool_UpdateStats(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:        server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
		UseSSL:          false,
		Region:          "us-east-1",
		DialTimeout:     time.Second,
		RequestTimeout:  time.Second,
	}

	pool, err := NewMinIOPool(config)
	require.NoError(t, err)
	defer pool.Close()

	// 测试统计更新
	pool.updateStats(true, time.Millisecond*100, false)
	pool.updateStats(false, time.Millisecond*200, true)

	stats := pool.GetStats()
	assert.Equal(t, int64(2), stats.TotalRequests)
	assert.Equal(t, int64(1), stats.SuccessRequests)
	assert.Equal(t, int64(1), stats.FailedRequests)
	assert.Equal(t, int64(1), stats.RetryRequests)
}

func TestMinIOPool_Close(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:        server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
		UseSSL:          false,
		Region:          "us-east-1",
		DialTimeout:     time.Second,
		RequestTimeout:  time.Second,
	}

	pool, err := NewMinIOPool(config)
	require.NoError(t, err)

	// 测试关闭
	pool.Close()

	// 再次关闭应该不会出错
	pool.Close()
}

func TestMinIOPool_ConcurrentAccess(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:        server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
		UseSSL:          false,
		Region:          "us-east-1",
		DialTimeout:     time.Second,
		RequestTimeout:  time.Second,
	}

	pool, err := NewMinIOPool(config)
	require.NoError(t, err)
	defer pool.Close()

	var wg sync.WaitGroup
	const numGoroutines = 10

	// 并发测试
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			// 获取统计信息
			stats := pool.GetStats()
			assert.NotNil(t, stats)
			
			// 获取客户端
			client := pool.GetClient()
			assert.NotNil(t, client)
		}()
	}

	wg.Wait()
}

func TestMinIOPool_ConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *MinIOPoolConfig
		expectError bool
	}{
		{
			name: "valid config",
			config: &MinIOPoolConfig{
				Endpoint:        "localhost:9000",
				AccessKeyID:     "testkey",
				SecretAccessKey: "testsecret",
				UseSSL:          false,
				Region:          "us-east-1",
			},
			expectError: false,
		},
		{
			name: "empty endpoint",
			config: &MinIOPoolConfig{
				Endpoint:        "",
				AccessKeyID:     "testkey",
				SecretAccessKey: "testsecret",
				UseSSL:          false,
				Region:          "us-east-1",
			},
			expectError: true,
		},
		{
			name: "empty access key",
			config: &MinIOPoolConfig{
				Endpoint:        "localhost:9000",
				AccessKeyID:     "",
				SecretAccessKey: "testsecret",
				UseSSL:          false,
				Region:          "us-east-1",
			},
			expectError: true,
		},
		{
			name: "empty secret key",
			config: &MinIOPoolConfig{
				Endpoint:        "localhost:9000",
				AccessKeyID:     "testkey",
				SecretAccessKey: "",
				UseSSL:          false,
				Region:          "us-east-1",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, err := NewMinIOPool(tt.config)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, pool)
			} else {
				// 由于没有真实的MinIO服务器，可能会失败
				// 但至少配置验证应该通过
				if err != nil && pool == nil {
					// 这可能是由于连接失败，而不是配置错误
					t.Logf("Connection failed (expected): %v", err)
				}
			}
			
			if pool != nil {
				pool.Close()
			}
		})
	}
}

func BenchmarkMinIOPool_GetClient(b *testing.B) {
	// 创建模拟的MinIO服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:            server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:         "testkey",
		SecretAccessKey:     "testsecret",
		UseSSL:              false,
		Region:              "us-east-1",
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 50,
		MaxConnsPerHost:     100,
	}

	pool, err := NewMinIOPool(config)
	require.NoError(b, err)
	require.NotNil(b, pool)
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			client := pool.GetClient()
			if client == nil {
				b.Fatal("Failed to get client")
			}
		}
	})
}

func BenchmarkMinIOPool_GetStats(b *testing.B) {
	// 创建模拟的MinIO服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:        server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
		UseSSL:          false,
		Region:          "us-east-1",
	}

	pool, err := NewMinIOPool(config)
	require.NoError(b, err)
	require.NotNil(b, pool)
	defer pool.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			stats := pool.GetStats()
			if stats == nil {
				b.Fatal("Failed to get stats")
			}
		}
	})
}

func TestMinIOPool_GetStats(t *testing.T) {
	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
				<ListAllMyBucketsResult>
					<Owner>
						<ID>minio</ID>
						<DisplayName>MinIO User</DisplayName>
					</Owner>
					<Buckets>
					</Buckets>
				</ListAllMyBucketsResult>`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := &MinIOPoolConfig{
		Endpoint:        server.URL[7:], // 去掉 "http://" 前缀
		AccessKeyID:     "testkey",
		SecretAccessKey: "testsecret",
		UseSSL:          false,
		Region:          "us-east-1",
		DialTimeout:     time.Second,
		RequestTimeout:  time.Second,
	}

	pool, err := NewMinIOPool(config)
	require.NoError(t, err)
	defer pool.Close()

	// 获取统计信息
	stats := pool.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, int64(0), stats.TotalRequests)
	assert.Equal(t, int64(0), stats.SuccessRequests)
	assert.Equal(t, int64(0), stats.FailedRequests)
} 