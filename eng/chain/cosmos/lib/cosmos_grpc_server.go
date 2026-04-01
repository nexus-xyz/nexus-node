package lib

import (
	"context"
	"fmt"
	"net"
	"time"

	"cosmossdk.io/log"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"nexus/x/gen/nexus/chain/v1/types"
)

const (
	// GRPCDefaultAddr is the default address for the Cosmos gRPC server.
	// This server receives inbound requests from Core to Cosmos.
	GRPCDefaultAddr = "0.0.0.0:50052"

	// CosmosGrpcAddrEnvVar is the environment variable to configure the Cosmos gRPC server address.
	CosmosGrpcAddrEnvVar = "COSMOS_GRPC_ADDR"
)

// CosmosServer is the interface for the Cosmos gRPC service.
type CosmosServer interface {
	HealthCheck(ctx context.Context, req *types.HealthCheckRequest) (*types.HealthCheckResponse, error)
}

// CosmosServerImpl implements the CosmosServer interface.
type CosmosServerImpl struct {
	types.UnimplementedCosmosServiceServer
	version    string
	getHeight  func() int64
	grpcServer *grpc.Server
	logger     log.Logger
}

// NewCosmosServer creates a new Cosmos service server implementation.
func NewCosmosServer(
	version string,
	getHeight func() int64,
	logger log.Logger,
) *CosmosServerImpl {
	return &CosmosServerImpl{
		version:   version,
		getHeight: getHeight,
		logger:    logger,
	}
}

// HealthCheck handles health check requests from the coprocessor.
func (s *CosmosServerImpl) HealthCheck(
	ctx context.Context, req *types.HealthCheckRequest,
) (*types.HealthCheckResponse, error) {
	s.logger.Debug("health check request", "sender_id", req.SenderId, "timestamp", req.Timestamp)
	resp := &types.HealthCheckResponse{
		SenderId:    req.SenderId,
		BlockHeight: s.getHeight(),
		Timestamp:   time.Now().UnixMilli(),
	}
	s.logger.Debug("health check response", "block_height", resp.BlockHeight)
	return resp, nil
}

// StartGRPCServer starts the gRPC server on the specified address.
// Returns the server instance, an error channel for async error handling, or an error if startup fails.
func StartGRPCServer(addr string, impl *CosmosServerImpl) (*grpc.Server, <-chan error, error) {
	jwtSecret, err := LoadJWTSecretFromEnv()
	if err != nil {
		return nil, nil, fmt.Errorf("JWT authentication required: %w", err)
	}
	return StartGRPCServerInternal(addr, impl, jwtSecret)
}

// StartGRPCServerInternal is the internal implementation for starting the gRPC server.
func StartGRPCServerInternal(
	addr string, impl *CosmosServerImpl, jwtSecret *JwtSecret,
) (*grpc.Server, <-chan error, error) {
	if addr == "" {
		addr = GRPCDefaultAddr
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Build server options with JWT authentication
	serverOpts := []grpc.ServerOption{
		grpc.UnaryInterceptor(auth.UnaryServerInterceptor(jwtSecret.AuthFunc())),
	}
	impl.logger.Info("JWT authentication enabled for Cosmos gRPC server")

	grpcServer := grpc.NewServer(serverOpts...)

	// Register service using the exported service descriptor for proper reflection support
	types.RegisterCosmosServiceServer(grpcServer, impl)

	// Enable gRPC reflection for grpcurl
	reflection.Register(grpcServer)

	impl.grpcServer = grpcServer

	errCh := make(chan error, 1)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			impl.logger.Error("gRPC server error", "error", err)
			errCh <- err
		}
		close(errCh)
	}()

	return grpcServer, errCh, nil
}

// Stop gracefully stops the gRPC server.
func (s *CosmosServerImpl) Stop() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

// MockCosmosServer is a mock implementation that returns errors.
type MockCosmosServer struct{}

// NewMockCosmosServer creates a mock Cosmos service server.
func NewMockCosmosServer() CosmosServer {
	return &MockCosmosServer{}
}

// HealthCheck returns an error indicating the server is not configured.
func (m *MockCosmosServer) HealthCheck(
	ctx context.Context, req *types.HealthCheckRequest,
) (*types.HealthCheckResponse, error) {
	return nil, fmt.Errorf("mock: Cosmos service server not configured")
}
