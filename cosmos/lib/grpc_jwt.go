package lib

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// JwtSecret encapsulates a JWT signing secret.
// This type prevents the raw secret from being passed around directly,
// reducing the risk of accidental copying or logging.
type JwtSecret struct {
	secret []byte
}

// NewJwtSecret creates a JwtSecret from raw bytes.
func NewJwtSecret(secret []byte) *JwtSecret {
	return &JwtSecret{secret: secret}
}

// GenerateToken creates a new JWT token for authentication.
// Includes the `iat` (issued-at) claim as required by the Engine API spec.
func (s *JwtSecret) GenerateToken() (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iat": time.Now().Unix(),
	})

	tokenString, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}

	return tokenString, nil
}

// ValidateToken verifies a JWT token is properly signed with HS256.
func (s *JwtSecret) ValidateToken(tokenString string) error {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Check HMAC method type
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		// Check exact algorithm is HS256 (not HS384 or HS512)
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected algorithm: expected HS256, got %s", token.Method.Alg())
		}
		return s.secret, nil
	})
	if err != nil {
		return fmt.Errorf("parse jwt: %w", err)
	}

	if !token.Valid {
		return fmt.Errorf("invalid jwt token")
	}

	return nil
}

// AuthFunc returns an AuthFunc for go-grpc-middleware.
// This is used with auth.UnaryServerInterceptor for server-side JWT validation.
func (s *JwtSecret) AuthFunc() auth.AuthFunc {
	return func(ctx context.Context) (context.Context, error) {
		token, err := auth.AuthFromMD(ctx, "bearer")
		if err != nil {
			return nil, err
		}
		if err := s.ValidateToken(token); err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
		}
		return ctx, nil
	}
}

// LoadJWTSecretFromEnv loads JWT secret from the GRPC_JWT_SECRET_PATH environment variable.
func LoadJWTSecretFromEnv() (*JwtSecret, error) {
	secretPath := os.Getenv(GRPCJWTSecretPathEnvVar)
	if secretPath == "" {
		return nil, fmt.Errorf("%s environment variable not set", GRPCJWTSecretPathEnvVar)
	}
	return LoadJWTSecret(secretPath)
}

// JWTUnaryClientInterceptor returns a gRPC unary client interceptor that adds
// a JWT token to outgoing requests. The token is added to the "authorization"
// metadata field with the "Bearer " prefix.
func JWTUnaryClientInterceptor(jwtSecret *JwtSecret) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		token, err := jwtSecret.GenerateToken()
		if err != nil {
			return status.Errorf(codes.Internal, "generate jwt: %v", err)
		}

		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
