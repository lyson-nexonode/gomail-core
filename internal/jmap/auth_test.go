package jmap

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateAndValidateToken verifies the full JWT token lifecycle.
func TestGenerateAndValidateToken(t *testing.T) {
	token, err := GenerateToken(42, "test@gomail.local")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := ValidateToken(token)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), claims.UserID)
	assert.Equal(t, "test@gomail.local", claims.Username)
}

// TestValidateTokenExpired verifies that expired tokens are rejected.
func TestValidateTokenExpired(t *testing.T) {
	// Build an already-expired token manually
	claims := Claims{
		UserID:   1,
		Username: "test@gomail.local",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Issuer:    "gomail-core",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(jwtSecret))
	require.NoError(t, err)

	_, err = ValidateToken(signed)
	assert.Error(t, err, "expired token must be rejected")
}

// TestValidateTokenTamperedSignature verifies that tampered tokens are rejected.
func TestValidateTokenTamperedSignature(t *testing.T) {
	token, err := GenerateToken(1, "test@gomail.local")
	require.NoError(t, err)

	// Tamper with the token by appending a character
	tampered := token + "x"
	_, err = ValidateToken(tampered)
	assert.Error(t, err, "tampered token must be rejected")
}

// TestExtractBearerToken verifies Bearer token extraction from HTTP headers.
func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
		ok       bool
	}{
		{
			name:     "valid Bearer token",
			header:   "Bearer mytoken123",
			expected: "mytoken123",
			ok:       true,
		},
		{
			name:   "missing header returns error",
			header: "",
			ok:     false,
		},
		{
			name:   "wrong scheme returns error",
			header: "Basic dXNlcjpwYXNz",
			ok:     false,
		},
		{
			name:   "malformed header returns error",
			header: "BearerNoSpace",
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/jmap", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			token, err := ExtractBearerToken(req)
			if tt.ok {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, token)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
