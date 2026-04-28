package lib

import (
	"context"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"nexus/x/gen/nexus/chain/v1/types"
)

// GRPCServerTestSuite tests the CosmosService gRPC server (inbound: Core -> Cosmos).
type GRPCServerTestSuite struct {
	suite.Suite

	server     *grpc.Server
	serverImpl *CosmosServerImpl
	serverAddr string
	errCh      <-chan error

	conn   *grpc.ClientConn
	client types.CosmosServiceClient

	jwtSecret *JwtSecret
	logger    log.Logger
}

func TestGRPCServerTestSuite(t *testing.T) {
	suite.Run(t, new(GRPCServerTestSuite))
}

func (s *GRPCServerTestSuite) SetupTest() {
	s.logger = log.NewNopLogger()

	// Generate random JWT secret for testing
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	s.Require().NoError(err)
	s.jwtSecret = NewJwtSecret(secret)

	// Get a free port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	s.serverAddr = lis.Addr().String()
	lis.Close()

	// Create server implementation with test block height
	blockHeight := int64(100)
	s.serverImpl = NewCosmosServer(
		"",
		func() int64 { return blockHeight },
		s.logger,
	)

	// Start server with JWT auth
	s.server, s.errCh, err = StartGRPCServerInternal(s.serverAddr, s.serverImpl, s.jwtSecret)
	s.Require().NoError(err)

	// Give the server time to start
	time.Sleep(50 * time.Millisecond)

	// Connect client with JWT auth
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.conn, err = grpc.DialContext(ctx, s.serverAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(JWTUnaryClientInterceptor(s.jwtSecret)),
		grpc.WithBlock(),
	)
	s.Require().NoError(err)

	s.client = types.NewCosmosServiceClient(s.conn)
}

func (s *GRPCServerTestSuite) TearDownTest() {
	if s.conn != nil {
		s.conn.Close()
	}
	if s.server != nil {
		s.server.GracefulStop()
	}
}

func (s *GRPCServerTestSuite) TestHealthCheckSuccess() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &types.HealthCheckRequest{
		SenderId:  "test-sender",
		Timestamp: time.Now().UnixMilli(),
	}

	resp, err := s.client.HealthCheck(ctx, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Equal("test-sender", resp.SenderId)
	s.Require().Equal(int64(100), resp.BlockHeight)
	s.Require().Greater(resp.Timestamp, int64(0))
}

func (s *GRPCServerTestSuite) TestHealthCheckWithDifferentSenderIds() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	senderIds := []string{"sender-1", "sender-2", "core-coprocessor", "test-123"}

	for _, senderId := range senderIds {
		req := &types.HealthCheckRequest{
			SenderId:  senderId,
			Timestamp: time.Now().UnixMilli(),
		}

		resp, err := s.client.HealthCheck(ctx, req)
		s.Require().NoError(err, "Failed for sender: %s", senderId)
		s.Require().Equal(senderId, resp.SenderId)
	}
}

func (s *GRPCServerTestSuite) TestConcurrentHealthCheckRequests() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	numRequests := 10
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := &types.HealthCheckRequest{
				SenderId:  "concurrent-sender",
				Timestamp: time.Now().UnixMilli(),
			}
			_, err := s.client.HealthCheck(ctx, req)
			results <- err
		}(i)
	}

	for i := 0; i < numRequests; i++ {
		err := <-results
		s.Require().NoError(err, "Concurrent request %d failed", i)
	}
}

func (s *GRPCServerTestSuite) TestServerGracefulShutdown() {
	// Server is already started in SetupTest
	// Verify we can health check before shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &types.HealthCheckRequest{
		SenderId:  "shutdown-test",
		Timestamp: time.Now().UnixMilli(),
	}

	resp, err := s.client.HealthCheck(ctx, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Graceful stop
	s.server.GracefulStop()
	s.server = nil // Prevent TearDownTest from stopping again

	// Verify error channel is closed (no error on graceful stop)
	select {
	case err := <-s.errCh:
		// grpc.Server.Serve returns nil on GracefulStop
		s.Require().Nil(err)
	case <-time.After(2 * time.Second):
		// Channel closed without error is also valid
	}
}

// CoreClientTestSuite tests the CoreClient gRPC client (outbound: Cosmos -> Core).
type CoreClientTestSuite struct {
	suite.Suite

	// Mock Core server
	mockServer     *grpc.Server
	mockServerAddr string
	mockImpl       *mockCoreServiceServer

	jwtSecret *JwtSecret
	logger    log.Logger
}

func TestCoreClientTestSuite(t *testing.T) {
	suite.Run(t, new(CoreClientTestSuite))
}

// mockCoreServiceServer implements the CoreService for testing.
type mockCoreServiceServer struct {
	types.UnimplementedCoreServiceServer

	newPayloadResponse        *types.NewPayloadResponse
	newPayloadError           error
	forkchoiceUpdatedResponse *types.ForkchoiceUpdatedResponse
	forkchoiceUpdatedError    error

	// Track calls
	newPayloadCalls        []*types.NewPayloadRequest
	forkchoiceUpdatedCalls []*types.ForkchoiceUpdatedRequest
}

func (m *mockCoreServiceServer) NewPayload(
	ctx context.Context, req *types.NewPayloadRequest,
) (*types.NewPayloadResponse, error) {
	m.newPayloadCalls = append(m.newPayloadCalls, req)
	if m.newPayloadError != nil {
		return nil, m.newPayloadError
	}
	return m.newPayloadResponse, nil
}

func (m *mockCoreServiceServer) ForkchoiceUpdated(
	ctx context.Context, req *types.ForkchoiceUpdatedRequest,
) (*types.ForkchoiceUpdatedResponse, error) {
	m.forkchoiceUpdatedCalls = append(m.forkchoiceUpdatedCalls, req)
	if m.forkchoiceUpdatedError != nil {
		return nil, m.forkchoiceUpdatedError
	}
	return m.forkchoiceUpdatedResponse, nil
}

func (s *CoreClientTestSuite) SetupTest() {
	s.logger = log.NewNopLogger()

	// Generate random JWT secret for testing
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	s.Require().NoError(err)
	s.jwtSecret = NewJwtSecret(secret)

	// Get a free port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	s.mockServerAddr = lis.Addr().String()

	// Create mock server
	s.mockImpl = &mockCoreServiceServer{
		newPayloadResponse: &types.NewPayloadResponse{
			Status:          types.PayloadStatus_PAYLOAD_STATUS_VALID,
			LatestValidHash: []byte{0x01, 0x02, 0x03},
		},
		forkchoiceUpdatedResponse: &types.ForkchoiceUpdatedResponse{
			PayloadStatus:   types.PayloadStatus_PAYLOAD_STATUS_VALID,
			LatestValidHash: []byte{0x04, 0x05, 0x06},
		},
	}

	s.mockServer = grpc.NewServer()
	types.RegisterCoreServiceServer(s.mockServer, s.mockImpl)

	// Start server in goroutine
	go func() {
		_ = s.mockServer.Serve(lis) // Error ignored; server stops on GracefulStop
	}()

	// Give the server time to start
	time.Sleep(50 * time.Millisecond)
}

func (s *CoreClientTestSuite) TearDownTest() {
	if s.mockServer != nil {
		s.mockServer.GracefulStop()
	}
}

func (s *CoreClientTestSuite) TestNewPayloadSuccess() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newCoreClientInternal(ctx, s.mockServerAddr, s.logger, s.jwtSecret)
	s.Require().NoError(err)
	defer client.Close()

	req := &types.NewPayloadRequest{
		Block: &types.Block{
			Header: &types.Header{
				Height:    100,
				Timestamp: uint64(time.Now().Unix()),
			},
			Transactions: []*types.SignedTransaction{
				{
					Transaction: &types.Transaction{
						Sender: []byte{0x01},
						Nonce:  1,
					},
					Signature: []byte{0x02},
				},
			},
		},
	}

	resp, err := client.NewPayload(ctx, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Equal(types.PayloadStatus_PAYLOAD_STATUS_VALID, resp.Status)
	s.Require().Equal([]byte{0x01, 0x02, 0x03}, resp.LatestValidHash)

	// Verify the request was received
	s.Require().Len(s.mockImpl.newPayloadCalls, 1)
	s.Require().Equal(uint64(100), s.mockImpl.newPayloadCalls[0].Block.Header.Height)
}

func (s *CoreClientTestSuite) TestForkchoiceUpdatedSuccess() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newCoreClientInternal(ctx, s.mockServerAddr, s.logger, s.jwtSecret)
	s.Require().NoError(err)
	defer client.Close()

	req := &types.ForkchoiceUpdatedRequest{
		ForkchoiceState: &types.ForkchoiceState{
			HeadBlockHash:      []byte{0x01, 0x02, 0x03},
			SafeBlockHash:      []byte{0x04, 0x05, 0x06},
			FinalizedBlockHash: []byte{0x07, 0x08, 0x09},
		},
	}

	resp, err := client.ForkchoiceUpdated(ctx, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Equal(types.PayloadStatus_PAYLOAD_STATUS_VALID, resp.PayloadStatus)

	// Verify the request was received
	s.Require().Len(s.mockImpl.forkchoiceUpdatedCalls, 1)
	s.Require().Equal([]byte{0x01, 0x02, 0x03}, s.mockImpl.forkchoiceUpdatedCalls[0].ForkchoiceState.HeadBlockHash)
}

func (s *CoreClientTestSuite) TestNewPayloadValidation() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newCoreClientInternal(ctx, s.mockServerAddr, s.logger, s.jwtSecret)
	s.Require().NoError(err)
	defer client.Close()

	// Test nil request block
	_, err = client.NewPayload(ctx, &types.NewPayloadRequest{})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "block and header are required")

	// Test nil header
	_, err = client.NewPayload(ctx, &types.NewPayloadRequest{
		Block: &types.Block{},
	})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "block and header are required")
}

func (s *CoreClientTestSuite) TestForkchoiceUpdatedValidation() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newCoreClientInternal(ctx, s.mockServerAddr, s.logger, s.jwtSecret)
	s.Require().NoError(err)
	defer client.Close()

	// Test nil forkchoice state
	_, err = client.ForkchoiceUpdated(ctx, &types.ForkchoiceUpdatedRequest{})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "forkchoice state is required")
}

func (s *CoreClientTestSuite) TestClientClose() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newCoreClientInternal(ctx, s.mockServerAddr, s.logger, s.jwtSecret)
	s.Require().NoError(err)

	// Close should not error
	err = client.Close()
	s.Require().NoError(err)
}

func (s *CoreClientTestSuite) TestClientConnectionFailure() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to connect to a non-existent server
	// grpc.NewClient doesn't actually connect until first RPC
	client, err := newCoreClientInternal(ctx, "127.0.0.1:1", s.logger, s.jwtSecret)
	s.Require().NoError(err) // NewClient succeeds even if server doesn't exist
	defer client.Close()

	// First RPC call should fail
	req := &types.NewPayloadRequest{
		Block: &types.Block{
			Header: &types.Header{Height: 1},
		},
	}
	_, err = client.NewPayload(ctx, req)
	s.Require().Error(err)
}
