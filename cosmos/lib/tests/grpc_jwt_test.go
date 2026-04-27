package lib_test

import (
	"context"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"nexus/lib"
	"nexus/x/gen/nexus/chain/v1/types"
)

// GRPCJWTTestSuite tests JWT authentication for gRPC connections.
type GRPCJWTTestSuite struct {
	suite.Suite

	jwtSecret *lib.JwtSecret
	logger    log.Logger
}

func TestGRPCJWTTestSuite(t *testing.T) {
	suite.Run(t, new(GRPCJWTTestSuite))
}

func (s *GRPCJWTTestSuite) SetupTest() {
	s.logger = log.NewNopLogger()
	// Use random 32-byte secret for testing
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	s.Require().NoError(err)
	s.jwtSecret = lib.NewJwtSecret(secret)
}

// startTestServer starts a test gRPC server and returns its address and a cleanup function.
func (s *GRPCJWTTestSuite) startTestServer() (string, *grpc.Server, func()) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	addr := lis.Addr().String()
	lis.Close()

	impl := lib.NewCosmosServer("", func() int64 { return 100 }, s.logger)
	server, errCh, err := lib.StartGRPCServerInternal(addr, impl, s.jwtSecret)
	s.Require().NoError(err)

	go func() { <-errCh }()
	time.Sleep(50 * time.Millisecond)

	return addr, server, func() { server.GracefulStop() }
}

// connectClient creates a gRPC connection to the given address.
// If jwtSecret is provided, JWT auth is enabled; otherwise connection is unauthenticated.
func (s *GRPCJWTTestSuite) connectClient(ctx context.Context, addr string, jwtSecret *lib.JwtSecret) *grpc.ClientConn {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	}
	if jwtSecret != nil {
		opts = append(opts, grpc.WithUnaryInterceptor(lib.JWTUnaryClientInterceptor(jwtSecret)))
	}

	conn, err := grpc.DialContext(ctx, addr, opts...)
	s.Require().NoError(err)
	return conn
}

func (s *GRPCJWTTestSuite) TestGenerateAndValidateJWT() {
	// Generate a token
	token, err := s.jwtSecret.GenerateToken()
	s.Require().NoError(err)
	s.Require().NotEmpty(token)

	// Validate the token
	err = s.jwtSecret.ValidateToken(token)
	s.Require().NoError(err)
}

func (s *GRPCJWTTestSuite) TestValidateJWTWithWrongSecret() {
	// Generate a token with one secret
	token, err := s.jwtSecret.GenerateToken()
	s.Require().NoError(err)

	// Try to validate with a different secret
	wrongSecret := make([]byte, 32)
	_, err = rand.Read(wrongSecret)
	s.Require().NoError(err)

	wrongJwtSecret := lib.NewJwtSecret(wrongSecret)
	err = wrongJwtSecret.ValidateToken(token)
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "parse jwt")
}

func (s *GRPCJWTTestSuite) TestValidateJWTWithInvalidToken() {
	err := s.jwtSecret.ValidateToken("invalid.token.here")
	s.Require().Error(err)
}

func (s *GRPCJWTTestSuite) TestServerRejectsUnauthenticatedRequest() {
	addr, _, cleanup := s.startTestServer()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect client WITHOUT JWT auth
	conn := s.connectClient(ctx, addr, nil)
	defer conn.Close()

	client := types.NewCosmosServiceClient(conn)

	// Request should fail with Unauthenticated
	req := &types.HealthCheckRequest{
		SenderId:  "test-sender",
		Timestamp: time.Now().UnixMilli(),
	}
	_, err := client.HealthCheck(ctx, req)
	s.Require().Error(err)

	st, ok := status.FromError(err)
	s.Require().True(ok)
	s.Require().Equal(codes.Unauthenticated, st.Code())
}

func (s *GRPCJWTTestSuite) TestAuthenticatedRequestSucceeds() {
	addr, _, cleanup := s.startTestServer()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect client WITH JWT auth
	conn := s.connectClient(ctx, addr, s.jwtSecret)
	defer conn.Close()

	client := types.NewCosmosServiceClient(conn)

	// Request should succeed
	req := &types.HealthCheckRequest{
		SenderId:  "test-sender",
		Timestamp: time.Now().UnixMilli(),
	}
	resp, err := client.HealthCheck(ctx, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.Require().Equal("test-sender", resp.SenderId)
	s.Require().Equal(int64(100), resp.BlockHeight)
}

func (s *GRPCJWTTestSuite) TestAuthenticatedRequestWithWrongSecretFails() {
	addr, _, cleanup := s.startTestServer()
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a different random secret for the client
	wrongSecret := make([]byte, 32)
	_, err := rand.Read(wrongSecret)
	s.Require().NoError(err)
	wrongJwtSecret := lib.NewJwtSecret(wrongSecret)

	// Connect client with WRONG JWT secret
	conn := s.connectClient(ctx, addr, wrongJwtSecret)
	defer conn.Close()

	client := types.NewCosmosServiceClient(conn)

	// Request should fail with Unauthenticated
	req := &types.HealthCheckRequest{
		SenderId:  "test-sender",
		Timestamp: time.Now().UnixMilli(),
	}
	_, err = client.HealthCheck(ctx, req)
	s.Require().Error(err)

	st, ok := status.FromError(err)
	s.Require().True(ok)
	s.Require().Equal(codes.Unauthenticated, st.Code())
}
