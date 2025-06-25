package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"minIODB/internal/buffer"
	"minIODB/internal/config"
	"minIODB/internal/coordinator"
	"minIODB/internal/discovery"
	"minIODB/internal/ingest"
	"minIODB/internal/logger"
	"minIODB/internal/query"
	"minIODB/internal/service"
	"minIODB/internal/storage"
	grpcTransport "minIODB/internal/transport/grpc"
	restTransport "minIODB/internal/transport/rest"
	pb "minIODB/api/proto/olap/v1"
	"github.com/minio/minio-go/v7"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志
	if err := logger.Init(cfg.Log); err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	logger.Info("Starting MinIODB server...")

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化存储层
	redisClient, err := storage.NewRedisClient(cfg.Redis)
	if err != nil {
		logger.Fatal("Failed to create Redis client", "error", err)
	}
	defer redisClient.Close()

	primaryMinio, err := storage.NewMinioClientWrapper(cfg.Minio)
	if err != nil {
		logger.Fatal("Failed to create primary MinIO client", "error", err)
	}

	var backupMinio storage.Uploader
	if cfg.Backup.Enabled {
		backupMinio, err = storage.NewMinioClientWrapper(cfg.Backup.Minio)
		if err != nil {
			logger.Fatal("Failed to create backup MinIO client", "error", err)
		}
	}

	// 初始化缓冲区
	bufferManager := buffer.NewManager(cfg.Buffer.MaxSize)

	// 初始化服务组件
	sharedBuffer := buffer.NewSharedBuffer(
		redisClient.GetClient(), 
		primaryMinio, 
		backupMinio, 
		cfg.Backup.Minio.Bucket, 
		cfg.Buffer.MaxSize, 
		time.Duration(cfg.Buffer.FlushTimeout)*time.Second,
	)
	
	ingesterService := ingest.NewIngester(sharedBuffer)
	querierService, err := query.NewQuerier(redisClient.GetClient(), primaryMinio, cfg.Minio, sharedBuffer)
	if err != nil {
		logger.Fatal("Failed to create querier service", "error", err)
	}

	// 初始化服务注册与发现
	serviceRegistry, err := discovery.NewServiceRegistry(*cfg, cfg.Server.NodeID, cfg.Server.GRPCPort)
	if err != nil {
		logger.Fatal("Failed to create service registry", "error", err)
	}

	// 启动服务注册
	if err := serviceRegistry.Start(); err != nil {
		logger.Fatal("Failed to start service registry", "error", err)
	}
	defer serviceRegistry.Stop()

	// 初始化协调器
	writeCoordinator := coordinator.NewWriteCoordinator(serviceRegistry)
	queryCoord := coordinator.NewQueryCoordinator(redisClient.GetClient(), serviceRegistry)

	// 启动监控服务器
	var monitoringServer *http.Server
	if cfg.Monitoring.Enabled {
		monitoringServer = startMonitoringServer(cfg)
	}

	// 创建gRPC传输层
	grpcServer := startGRPCServer(cfg, ingesterService, querierService, writeCoordinator, queryCoord, redisClient, primaryMinio, backupMinio)

	// 启动REST服务器
	restServer := startRESTServer(cfg, ingesterService, querierService, writeCoordinator, queryCoord, redisClient, primaryMinio, backupMinio, bufferManager)

	// 启动缓冲区刷新goroutine
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.Buffer.FlushTimeout) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := bufferManager.Flush(func(data []buffer.DataPoint) error {
					// 批量写入数据
					for _, point := range data {
						dataRow := buffer.DataRow{
							ID:        point.ID,
							Timestamp: point.Timestamp.UnixNano(),
							Payload:   string(point.Data),
						}
						sharedBuffer.Add(dataRow)
					}
					return nil
				}); err != nil {
					logger.Error("Failed to flush buffer", "error", err)
				}
			}
		}
	}()

	// 启动备份goroutine
	if cfg.Backup.Enabled && backupMinio != nil {
		go startBackupRoutine(primaryMinio, backupMinio, *cfg)
	}

	logger.Info("MinIODB server started successfully")

	// 等待中断信号
	waitForShutdown(ctx, cancel, grpcServer, restServer, monitoringServer)

	logger.Info("MinIODB server stopped")
}

func startGRPCServer(cfg *config.Config, ingester *ingest.Ingester, querier *query.Querier, writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator, redisClient *storage.RedisClient, primaryMinio, backupMinio storage.Uploader) *grpc.Server {
	// 创建统一的service层
	olapService, err := service.NewOlapService(*cfg, ingester, querier, redisClient.GetClient(), primaryMinio, backupMinio)
	if err != nil {
		logger.Fatal("Failed to create OLAP service", "error", err)
	}
	
	// 创建gRPC服务
	grpcService, err := grpcTransport.NewServer(olapService, *cfg)
	if err != nil {
		logger.Fatal("Failed to create gRPC service", "error", err)
	}
	
	grpcServer := grpc.NewServer()
	
	// 注册服务
	pb.RegisterOlapServiceServer(grpcServer, grpcService)
	
	// 启用反射（用于调试）
	reflection.Register(grpcServer)

	// 启动gRPC服务器
	go func() {
		lis, err := net.Listen("tcp", cfg.Server.GRPCPort)
		if err != nil {
			logger.Fatal("Failed to listen on gRPC port", "port", cfg.Server.GRPCPort, "error", err)
		}

		logger.Info("gRPC server starting", "port", cfg.Server.GRPCPort)
		if err := grpcServer.Serve(lis); err != nil {
			logger.Fatal("Failed to serve gRPC", "error", err)
		}
	}()

	return grpcServer
}

func startRESTServer(cfg *config.Config, ingester *ingest.Ingester, querier *query.Querier, writeCoord *coordinator.WriteCoordinator, queryCoord *coordinator.QueryCoordinator, redisClient *storage.RedisClient, primaryMinio, backupMinio storage.Uploader, bufferManager *buffer.Manager) *restTransport.Server {
	// 创建统一的service层
	olapService, err := service.NewOlapService(*cfg, ingester, querier, redisClient.GetClient(), primaryMinio, backupMinio)
	if err != nil {
		logger.Fatal("Failed to create OLAP service", "error", err)
	}

	// 创建REST服务器
	restServer := restTransport.NewServer(olapService, cfg)
	
	// 设置协调器
	restServer.SetCoordinators(writeCoord, queryCoord)

	// 启动服务器
	go func() {
		logger.Info("REST server listening", "port", cfg.Server.RESTPort)
		if err := restServer.Start(cfg.Server.RESTPort); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start REST server", "error", err)
		}
	}()

	return restServer
}

func startMonitoringServer(cfg *config.Config) *http.Server {
	mux := http.NewServeMux()
	mux.Handle(cfg.Monitoring.Path, promhttp.Handler())
	
	// 健康检查端点
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	server := &http.Server{
		Addr:    cfg.Monitoring.Port,
		Handler: mux,
	}

	go func() {
		logger.Info("Monitoring server listening", "port", cfg.Monitoring.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start monitoring server", "error", err)
		}
	}()

	return server
}

func startBackupRoutine(primaryMinio, backupMinio storage.Uploader, cfg config.Config) {
	logger.GetLogger().Info("Starting backup routine")
	
	ticker := time.NewTicker(time.Duration(cfg.Backup.Interval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		logger.GetLogger().Info("Starting backup process")
		
		// 列出主存储中的所有对象
		objects, err := primaryMinio.ListObjectsSimple(context.Background(), cfg.Minio.Bucket, minio.ListObjectsOptions{})
		if err != nil {
			logger.GetLogger().Error("Failed to list objects from primary storage", "error", err)
			continue
		}

		logger.GetLogger().Info("Found objects to backup", "count", len(objects))

		// 备份每个对象
		for _, obj := range objects {
			// 检查备份存储中是否已存在该对象
			exists, err := backupMinio.ObjectExists(context.Background(), cfg.Backup.Minio.Bucket, obj.Name)
			if err != nil {
				logger.GetLogger().Error("Failed to check object existence in backup", "error", err, "object", obj.Name)
				continue
			}

			if exists {
				logger.GetLogger().Debug("Object already exists in backup", "object", obj.Name)
				continue
			}

			// 从主存储获取对象数据
			data, err := primaryMinio.GetObject(context.Background(), cfg.Minio.Bucket, obj.Name, minio.GetObjectOptions{})
			if err != nil {
				logger.GetLogger().Error("Failed to get object from primary storage", "error", err, "object", obj.Name)
				continue
			}

			// 上传到备份存储
			reader := bytes.NewReader(data)
			_, err = backupMinio.PutObject(context.Background(), cfg.Backup.Minio.Bucket, obj.Name, reader, int64(len(data)), minio.PutObjectOptions{})
			if err != nil {
				logger.GetLogger().Error("Failed to upload object to backup storage", "error", err, "object", obj.Name)
				continue
			}

			logger.GetLogger().Info("Successfully backed up object", "object", obj.Name, "size", len(data))
		}

		logger.GetLogger().Info("Backup process completed", "objects_processed", len(objects))
	}
}

func waitForShutdown(ctx context.Context, cancel context.CancelFunc, grpcServer *grpc.Server, restServer *restTransport.Server, monitoringServer *http.Server) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	<-c
	logger.Info("Shutting down servers...")

	// 创建超时上下文
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// 使用WaitGroup等待所有服务器关闭
	var wg sync.WaitGroup

	// 关闭gRPC服务器
	wg.Add(1)
	go func() {
		defer wg.Done()
		grpcServer.GracefulStop()
		logger.Info("gRPC server stopped")
	}()

	// 关闭REST服务器
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := restServer.Stop(shutdownCtx); err != nil {
			logger.Error("Failed to stop REST server", "error", err)
		}
		logger.Info("REST server stopped")
	}()

	// 关闭监控服务器
	if monitoringServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := monitoringServer.Shutdown(shutdownCtx); err != nil {
				logger.Error("Failed to stop monitoring server", "error", err)
			}
			logger.Info("Monitoring server stopped")
		}()
	}

	// 等待所有服务器关闭或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("All servers stopped gracefully")
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timeout exceeded, forcing exit")
	}

	cancel()
}
