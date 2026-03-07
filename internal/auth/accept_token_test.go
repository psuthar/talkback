package auth

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueAndValidateAcceptToken(t *testing.T) {
	secret := "test-secret-for-accept-token"
	os.Setenv("ACCEPT_TOKEN_SECRET", secret)
	defer os.Unsetenv("ACCEPT_TOKEN_SECRET")

	userID := uuid.New()
	tok, err := IssueAcceptToken(userID)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	got, err := ValidateAcceptToken(tok)
	require.NoError(t, err)
	assert.Equal(t, userID, got)
}

func TestValidateAcceptToken_Empty(t *testing.T) {
	_, err := ValidateAcceptToken("")
	assert.Error(t, err)
}

func TestValidateAcceptToken_Invalid(t *testing.T) {
	os.Setenv("ACCEPT_TOKEN_SECRET", "secret")
	defer os.Unsetenv("ACCEPT_TOKEN_SECRET")
	_, err := ValidateAcceptToken("invalid-token")
	assert.Error(t, err)
}
