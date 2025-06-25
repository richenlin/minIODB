package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	olapv1 "minIODB/api/proto/olap/v1"
	"minIODB/internal/config"
	"minIODB/internal/discovery"
	"minIODB/internal/storage"
	grpc_transport "minIODB/internal/transport/grpc"

	"github.com/minio/minio-go/v7"
	"google.golang.org/grpc"
)

func main() {
	// Load configuration
	cfg, err := config.LoadConfig(".")
	if err != nil {
		log.Fatalf("could not load config: %v", err)
	}

	// Initialize Redis client
	redisClient := discovery.NewRedisClient(cfg.Redis)
	if err := discovery.PingRedis(redisClient); err != nil {
		log.Fatalf("could not connect to redis: %v", err)
	}
	log.Println("Successfully connected to Redis")

	// Initialize MinIO client
	minioClient, err := storage.NewMinioClient(cfg.Minio)
	if err != nil {
		log.Fatalf("could not create minio client: %v", err)
	}
	// Check connection
	_, err = minioClient.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("could not connect to minio: %v", err)
	}
	log.Println("Successfully connected to MinIO")

	// Initialize Backup MinIO client if enabled
	var backupMinioClient *minio.Client
	if cfg.Backup.Enabled {
		var err error
		backupMinioClient, err = storage.NewMinioClient(cfg.Backup.Minio)
		if err != nil {
			log.Fatalf("could not create backup minio client: %v", err)
		}
		// Check connection to backup
		_, err = backupMinioClient.ListBuckets(context.Background())
		if err != nil {
			log.Fatalf("could not connect to backup minio: %v", err)
		}
		log.Println("Successfully connected to Backup MinIO")
	}

	fmt.Println("Starting OLAP Server...")

	// Create and register the service, passing the backup client
	olapServer, err := grpc_transport.NewServer(redisClient, minioClient, backupMinioClient, cfg)
	if err != nil {
		log.Fatalf("failed to create grpc server: %v", err)
	}

	// Start gRPC server in a goroutine
	lis, err := net.Listen("tcp", cfg.Server.GrpcPort)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	olapv1.RegisterOlapServiceServer(s, olapServer)

	go func() {
		log.Printf("server listening at %v", lis.Addr())
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Graceful shutdown
	s.GracefulStop()
	olapServer.Stop()
	log.Println("Server gracefully stopped")
}
