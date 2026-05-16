// SCRUM-427: persistence test for the Microsoft Graph refresh-token
// rotation flow. Graph rotates the refresh token on use; the rotation
// path in teams_auth.go calls UpdateTeamsConnectionTokens with the new
// pair. We can't easily mock the Graph HTTP without refactoring
// teamsTokenURL to take an override (out of scope for SCRUM-427), so we
// test the persistence boundary directly: when UpdateTeamsConnectionTokens
// is called with a different refresh-token blob, the row reflects the
// new bytes — exact behavior the rotation flow depends on.
package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTeamsConnectionTokens_RotatesRefreshToken(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	creatorIdentityID := "creator-" + uuid.NewString() + "@example.com"
	initialAccess := []byte("encrypted-access-v1")
	initialRefresh := []byte("encrypted-refresh-v1")
	initialExpires := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	conn := &models.TeamsConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creatorIdentityID,
		TenantID:              "tenant-1",
		TeamsUserID:           "user-1",
		TeamsUserEmail:        stringPtr("user@example.com"),
		AccessTokenEncrypted:  initialAccess,
		RefreshTokenEncrypted: initialRefresh,
		ExpiresAt:             initialExpires,
	}
	require.NoError(t, db.CreateTeamsConnection(ctx, conn))

	// Simulate a Graph refresh: tenant returned a new access AND a new
	// refresh token (rotated). The rotation path in teams_auth.go calls
	// UpdateTeamsConnectionTokens with the rotated pair.
	rotatedAccess := []byte("encrypted-access-v2-rotated")
	rotatedRefresh := []byte("encrypted-refresh-v2-rotated")
	rotatedExpires := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, db.UpdateTeamsConnectionTokens(ctx, creatorIdentityID, rotatedAccess, rotatedRefresh, rotatedExpires))

	got, err := db.GetTeamsConnectionByCreatorIdentity(ctx, creatorIdentityID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, rotatedAccess, got.AccessTokenEncrypted, "access token must be the rotated bytes")
	assert.Equal(t, rotatedRefresh, got.RefreshTokenEncrypted, "refresh token must be the rotated bytes — not the original")
	assert.WithinDuration(t, rotatedExpires, got.ExpiresAt, time.Second)
}

// Graph occasionally omits the rotated refresh and the caller must keep
// the previous one (teams_auth.go line ~299 falls back to the existing
// blob). This test pins the "caller passes the OLD refresh blob"
// behavior so we'd catch a refactor that accidentally clears the column.
func TestUpdateTeamsConnectionTokens_PreservesRefreshWhenCallerPassesSameBlob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	creatorIdentityID := "creator-" + uuid.NewString() + "@example.com"
	initialAccess := []byte("encrypted-access-v1")
	initialRefresh := []byte("encrypted-refresh-v1")
	initialExpires := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

	conn := &models.TeamsConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creatorIdentityID,
		TenantID:              "tenant-1",
		TeamsUserID:           "user-1",
		TeamsUserEmail:        stringPtr("user@example.com"),
		AccessTokenEncrypted:  initialAccess,
		RefreshTokenEncrypted: initialRefresh,
		ExpiresAt:             initialExpires,
	}
	require.NoError(t, db.CreateTeamsConnection(ctx, conn))

	// Graph returned access only — caller passes the OLD refresh blob.
	rotatedAccess := []byte("encrypted-access-v2-rotated")
	rotatedExpires := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, db.UpdateTeamsConnectionTokens(ctx, creatorIdentityID, rotatedAccess, initialRefresh, rotatedExpires))

	got, err := db.GetTeamsConnectionByCreatorIdentity(ctx, creatorIdentityID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, initialRefresh, got.RefreshTokenEncrypted, "refresh must remain v1 when caller passes it through")
}
