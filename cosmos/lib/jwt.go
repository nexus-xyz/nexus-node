package lib

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
)

type jwtClient struct {
	transport http.RoundTripper
	jwtSecret *JwtSecret
}

func NewJwtClient(transport http.RoundTripper, jwtSecret *JwtSecret) *jwtClient {
	return &jwtClient{
		transport: transport,
		jwtSecret: jwtSecret,
	}
}

func (c *jwtClient) RoundTrip(req *http.Request) (*http.Response, error) {
	tokenString, err := c.jwtSecret.GenerateToken()
	if err != nil {
		return nil, fmt.Errorf("jwt token string: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tokenString)

	response, err := c.transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}

	return response, nil
}

func LoadJWTSecret(path string) (*JwtSecret, error) {
	secret, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	secret = bytes.TrimSpace(secret)
	secret = bytes.TrimPrefix(secret, []byte("0x"))

	if len(secret) != 64 {
		return nil, fmt.Errorf("jwt secret must be 64 hex chars, got %d", len(secret))
	}

	secretBytes, err := hex.DecodeString(string(secret))
	if err != nil {
		return nil, err
	}

	return NewJwtSecret(secretBytes), nil
}
