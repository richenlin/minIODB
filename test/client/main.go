package main

import (
	"context"
	"log"
	"time"

	olapv1 "minIODB/api/proto/olap/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	conn, err := grpc.Dial("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := olapv1.NewOlapServiceClient(conn)

	// 演示写入数据
	testWrite(c)

	// 演示手动备份
	testBackup(c)

	// 演示数据恢复
	testRecover(c)
}

func testWrite(c olapv1.OlapServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	payload, err := structpb.NewStruct(map[string]interface{}{
		"key1": "value1",
		"key2": 123.45,
		"key3": true,
	})
	if err != nil {
		log.Fatalf("failed to create payload: %v", err)
	}

	req := &olapv1.WriteRequest{
		Id:        "test-id-123",
		Timestamp: timestamppb.Now(),
		Payload:   payload,
	}

	res, err := c.Write(ctx, req)
	if err != nil {
		log.Fatalf("could not write: %v", err)
	}

	log.Printf("Write Response: Success=%t, Message=%s", res.Success, res.Message)
}

func testBackup(c olapv1.OlapServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := &olapv1.TriggerBackupRequest{
		Id:  "test-id-123",
		Day: time.Now().Format("2006-01-02"),
	}

	res, err := c.TriggerBackup(ctx, req)
	if err != nil {
		log.Fatalf("could not trigger backup: %v", err)
	}

	log.Printf("Backup Response: Success=%t, Message=%s, FilesBackedUp=%d",
		res.Success, res.Message, res.FilesBackedUp)
}

func testRecover(c olapv1.OlapServiceClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 测试按节点ID恢复
	log.Println("Testing recovery by node ID...")
	nodeRecoverReq := &olapv1.RecoverDataRequest{
		RecoveryMode: &olapv1.RecoverDataRequest_NodeId{
			NodeId: "node-2",
		},
		ForceOverwrite: true,
	}

	nodeRes, err := c.RecoverData(ctx, nodeRecoverReq)
	if err != nil {
		log.Printf("Node recovery failed: %v", err)
	} else {
		log.Printf("Node Recovery Response: Success=%t, Message=%s, FilesRecovered=%d, RecoveredKeys=%v",
			nodeRes.Success, nodeRes.Message, nodeRes.FilesRecovered, nodeRes.RecoveredKeys)
	}

	// 测试按时间范围恢复
	log.Println("Testing recovery by time range...")
	timeRecoverReq := &olapv1.RecoverDataRequest{
		RecoveryMode: &olapv1.RecoverDataRequest_TimeRange{
			TimeRange: &olapv1.TimeRangeFilter{
				StartDate: "2023-11-01",
				EndDate:   "2023-11-02",
				Ids:       []string{"test-id-123"},
			},
		},
		ForceOverwrite: false,
	}

	timeRes, err := c.RecoverData(ctx, timeRecoverReq)
	if err != nil {
		log.Printf("Time range recovery failed: %v", err)
	} else {
		log.Printf("Time Recovery Response: Success=%t, Message=%s, FilesRecovered=%d, RecoveredKeys=%v",
			timeRes.Success, timeRes.Message, timeRes.FilesRecovered, timeRes.RecoveredKeys)
	}

	// 测试按ID范围恢复
	log.Println("Testing recovery by ID range...")
	idRecoverReq := &olapv1.RecoverDataRequest{
		RecoveryMode: &olapv1.RecoverDataRequest_IdRange{
			IdRange: &olapv1.IdRangeFilter{
				Ids: []string{"test-id-123", "user-456"},
			},
		},
		ForceOverwrite: false,
	}

	idRes, err := c.RecoverData(ctx, idRecoverReq)
	if err != nil {
		log.Printf("ID range recovery failed: %v", err)
	} else {
		log.Printf("ID Recovery Response: Success=%t, Message=%s, FilesRecovered=%d, RecoveredKeys=%v",
			idRes.Success, idRes.Message, idRes.FilesRecovered, idRes.RecoveredKeys)
	}
}
