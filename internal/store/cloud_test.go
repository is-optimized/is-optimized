package store

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"

	"cloud.google.com/go/firestore"
	pb "cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// This local protocol fixture checks the actual SDK write requests. It does not
// assert provider billing or replace a Firestore integration test.
type transactionFixture struct {
	pb.UnimplementedFirestoreServer
	writes atomic.Int64
}

func (*transactionFixture) BeginTransaction(context.Context, *pb.BeginTransactionRequest) (*pb.BeginTransactionResponse, error) {
	return &pb.BeginTransactionResponse{Transaction: []byte("fixture")}, nil
}
func (*transactionFixture) BatchGetDocuments(req *pb.BatchGetDocumentsRequest, stream pb.Firestore_BatchGetDocumentsServer) error {
	now := timestamppb.Now()
	return stream.Send(&pb.BatchGetDocumentsResponse{ReadTime: now, Result: &pb.BatchGetDocumentsResponse_Found{Found: &pb.Document{
		Name: req.Documents[0], CreateTime: now, UpdateTime: now,
		Fields: map[string]*pb.Value{"json": {ValueType: &pb.Value_StringValue{StringValue: `{"count":1}`}}},
	}}})
}
func (f *transactionFixture) Commit(_ context.Context, req *pb.CommitRequest) (*pb.CommitResponse, error) {
	f.writes.Add(int64(len(req.Writes)))
	return &pb.CommitResponse{CommitTime: timestamppb.Now()}, nil
}

func TestCloudUnchangedUpdatesSendNoDocumentWrites(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	fixture := &transactionFixture{}
	pb.RegisterFirestoreServer(server, fixture)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///fixture", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctx := context.Background()
	client, err := firestore.NewClient(ctx, "fixture-project", option.WithGRPCConn(conn))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	db := &Cloud{db: client}
	for range 20 {
		err := db.Update(ctx, "record", func(raw json.RawMessage) (any, error) {
			var value map[string]int
			err := json.Unmarshal(raw, &value)
			return value, err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := fixture.writes.Load(); n != 0 {
		t.Fatal("no-op updates sent document writes", n)
	}
	if err := db.Update(ctx, "record", func(json.RawMessage) (any, error) { return map[string]int{"count": 2}, nil }); err != nil {
		t.Fatal(err)
	}
	if n := fixture.writes.Load(); n != 1 {
		t.Fatal("changed update did not send exactly one write", n)
	}
}
