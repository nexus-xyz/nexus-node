package lib

import (
	"context"
	"fmt"
	"os"

	"cosmossdk.io/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"nexus/x/gen/nexus/chain/v1/types"
)

const (
	// CoreDefaultAddr is the default address for the Core engine gRPC server.
	CoreDefaultAddr = "localhost:50051"
	// CoreAddrEnvVar is the environment variable for Core gRPC address.
	CoreAddrEnvVar = "CORE_GRPC_ADDR"
	// CoreGrpcServerEnabledEnvVar is the environment variable to enable the grpc server and the core client
	// communication.
	CoreGrpcServerEnabledEnvVar = "GRPC_SERVER_ENABLED"
	// GRPCJWTSecretPathEnvVar is the environment variable for the path to the JWT secret file
	// used for gRPC authentication between Cosmos and Core.
	// nolint:gosec // G101: This is an environment variable name, not a hardcoded credential.
	GRPCJWTSecretPathEnvVar = "GRPC_JWT_SECRET_PATH"
)

// GetCoreAddr returns the Core gRPC address from env or default.
func GetCoreAddr() string {
	if addr := os.Getenv(CoreAddrEnvVar); addr != "" {
		return addr
	}
	return CoreDefaultAddr
}

// CoreClient provides methods for communicating with Core's engine service.
// This client performs outbound requests from Cosmos to Core.
type CoreClient interface {
	// NewPayload submits a new block payload to Core for execution.
	NewPayload(ctx context.Context, req *types.NewPayloadRequest) (*types.NewPayloadResponse, error)
	// ForkchoiceUpdated signals fork choice updates to Core.
	ForkchoiceUpdated(
		ctx context.Context, req *types.ForkchoiceUpdatedRequest,
	) (*types.ForkchoiceUpdatedResponse, error)
	// Close closes the underlying gRPC connection.
	Close() error
}

type coreClient struct {
	conn   *grpc.ClientConn
	client types.CoreServiceClient
	logger log.Logger
}

// NewCoreClient creates a new Core engine client.
// JWT authentication is mandatory and loaded from the GRPC_JWT_SECRET_PATH environment variable.
func NewCoreClient(ctx context.Context, addr string, logger log.Logger) (CoreClient, error) {
	jwtSecret, err := LoadJWTSecretFromEnv()
	if err != nil {
		return nil, fmt.Errorf("JWT authentication required: %w", err)
	}
	return newCoreClientInternal(ctx, addr, logger, jwtSecret)
}

// newCoreClientInternal is the internal implementation for creating a Core client.
func newCoreClientInternal(
	ctx context.Context, addr string, logger log.Logger, jwtSecret *JwtSecret,
) (CoreClient, error) {
	if addr == "" {
		addr = CoreDefaultAddr
	}

	// Build dial options with JWT authentication
	opts := []grpc.DialOption{
		// TODO: Add TLS support for production deployments. Using insecure
		// credentials is acceptable for local development only.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(JWTUnaryClientInterceptor(jwtSecret)),
	}
	logger.Info("JWT authentication enabled for Core client")

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial grpc: %w", err)
	}

	return &coreClient{
		conn:   conn,
		client: types.NewCoreServiceClient(conn),
		logger: logger,
	}, nil
}

func (c *coreClient) NewPayload(
	ctx context.Context, req *types.NewPayloadRequest,
) (*types.NewPayloadResponse, error) {
	// Validate the request payload.
	if req == nil || req.Block == nil || req.Block.Header == nil {
		return nil, fmt.Errorf("new payload: block and header are required")
	}

	c.logger.Debug("sending NewPayload", "height", req.Block.Header.Height)

	resp, err := c.client.NewPayload(ctx, req)
	if err != nil {
		c.logger.Error("NewPayload failed", "error", err)
		return nil, fmt.Errorf("new payload: %w", err)
	}

	c.logger.Debug("NewPayload response", "status", resp.Status)
	return resp, nil
}

func (c *coreClient) ForkchoiceUpdated(
	ctx context.Context,
	req *types.ForkchoiceUpdatedRequest,
) (*types.ForkchoiceUpdatedResponse, error) {
	if req == nil || req.ForkchoiceState == nil {
		return nil, fmt.Errorf("forkchoice updated: forkchoice state is required")
	}

	c.logger.Debug("sending ForkchoiceUpdated")

	resp, err := c.client.ForkchoiceUpdated(ctx, req)
	if err != nil {
		c.logger.Error("ForkchoiceUpdated failed", "error", err)
		return nil, fmt.Errorf("forkchoice updated: %w", err)
	}

	c.logger.Debug("ForkchoiceUpdated response", "status", resp.PayloadStatus)
	return resp, nil
}

func (c *coreClient) Close() error {
	return c.conn.Close()
}
