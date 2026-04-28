package lib

import (
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nexus/lib"

	"github.com/stretchr/testify/require"
)

func TestLoadJWTSecret(t *testing.T) {
	tempDir := t.TempDir()

	t.Run("valid jwt secret", func(t *testing.T) {
		jwtFile := filepath.Join(tempDir, "jwt.hex")
		jwtSecret := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		err := os.WriteFile(jwtFile, []byte(jwtSecret), 0644)
		require.NoError(t, err)

		secret, err := lib.LoadJWTSecret(jwtFile)
		require.NoError(t, err)
		require.NotNil(t, secret)
	})

	t.Run("jwt secret with 0x prefix", func(t *testing.T) {
		jwtFile := filepath.Join(tempDir, "jwt_with_prefix.hex")
		jwtSecret := "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		err := os.WriteFile(jwtFile, []byte(jwtSecret), 0644)
		require.NoError(t, err)

		secret, err := lib.LoadJWTSecret(jwtFile)
		require.NoError(t, err)
		require.NotNil(t, secret)
	})

	t.Run("jwt secret with whitespace", func(t *testing.T) {
		jwtFile := filepath.Join(tempDir, "jwt_whitespace.hex")
		jwtSecret := "  0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef  \n"
		err := os.WriteFile(jwtFile, []byte(jwtSecret), 0644)
		require.NoError(t, err)

		secret, err := lib.LoadJWTSecret(jwtFile)
		require.NoError(t, err)
		require.NotNil(t, secret)
	})

	t.Run("invalid jwt secret length", func(t *testing.T) {
		jwtFile := filepath.Join(tempDir, "jwt_short.hex")
		jwtSecret := "0123456789abcdef" // Too short
		err := os.WriteFile(jwtFile, []byte(jwtSecret), 0644)
		require.NoError(t, err)

		_, err = lib.LoadJWTSecret(jwtFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "jwt secret must be 64 hex chars")
	})

	t.Run("invalid hex characters", func(t *testing.T) {
		jwtFile := filepath.Join(tempDir, "jwt_invalid.hex")
		jwtSecret := "0123456789abcdefghij0123456789abcdef0123456789abcdef0123456789ab" // Invalid hex chars
		err := os.WriteFile(jwtFile, []byte(jwtSecret), 0644)
		require.NoError(t, err)

		_, err = lib.LoadJWTSecret(jwtFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid byte")
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := lib.LoadJWTSecret("nonexistent.hex")
		require.Error(t, err)
	})
}

func TestJWTClient(t *testing.T) {
	secret := make([]byte, 32)
	_, err := rand.Read(secret)
	require.NoError(t, err)
	jwtSecret := lib.NewJwtSecret(secret)

	// Create a test server that verifies JWT tokens
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		require.Contains(t, auth, "Bearer")

		// Verify JWT token is present (we don't validate the signature here)
		token := strings.TrimPrefix(auth, "Bearer ")
		require.NotEmpty(t, token)
		require.Contains(t, token, ".") // JWT has dots separating parts

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	// Create JWT client
	transport := lib.NewJwtClient(http.DefaultTransport, jwtSecret)
	client := &http.Client{Transport: transport}

	// Make a request
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "OK", string(body))
}
