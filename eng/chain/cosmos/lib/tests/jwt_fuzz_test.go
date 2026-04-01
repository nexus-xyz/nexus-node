package lib

import (
	"os"
	"path/filepath"
	"testing"

	"nexus/lib"
)

// FuzzLoadJWTSecret tests JWT secret parsing with random inputs
func FuzzLoadJWTSecret(f *testing.F) {
	// Minimal seed corpus - let fuzzer generate random data
	f.Add("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") // One valid example
	f.Add("")                                                                 // One edge case

	f.Fuzz(func(t *testing.T, input string) {
		// Create temporary file with fuzz input
		tempDir := t.TempDir()
		jwtFile := filepath.Join(tempDir, "jwt.hex")

		// Write the random fuzz input to file
		err := os.WriteFile(jwtFile, []byte(input), 0644)
		if err != nil {
			t.Skip("Failed to write temp file")
		}

		// Test LoadJWTSecret - should not panic with any input
		secret, err := lib.LoadJWTSecret(jwtFile)

		// If no error, secret should be non-nil
		if err == nil && secret == nil {
			t.Errorf("Expected non-nil JwtSecret when no error")
		}
		// We don't fail on errors since most random inputs will be invalid
	})
}
