package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func TestGoogleMeetConnections_CRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	creator := "creator-google-meet@example.com"

	// Get on empty returns (nil, nil) — same shape as zoom/teams.
	conn, err := db.GetGoogleMeetConnectionByCreatorIdentity(ctx, creator)
	require.NoError(t, err)
	require.Nil(t, conn)

	// Create.
	email := "alice@workspace.example"
	scope := "openid email profile https://www.googleapis.com/auth/meetings.space.readonly https://www.googleapis.com/auth/drive.readonly"
	eligible := true
	conn = &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creator,
		GoogleUserID:          "google-sub-123",
		GoogleUserEmail:       &email,
		GrantedScope:          &scope,
		WorkspaceEligible:     &eligible,
		AccessTokenEncrypted:  []byte("access-cipher-1"),
		RefreshTokenEncrypted: []byte("refresh-cipher-1"),
		ExpiresAt:             time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}
	require.NoError(t, db.CreateGoogleMeetConnection(ctx, conn))
	require.False(t, conn.CreatedAt.IsZero())
	require.False(t, conn.UpdatedAt.IsZero())

	// Get round-trips fields.
	got, err := db.GetGoogleMeetConnectionByCreatorIdentity(ctx, creator)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, conn.ID, got.ID)
	require.Equal(t, conn.GoogleUserID, got.GoogleUserID)
	require.NotNil(t, got.GoogleUserEmail)
	require.Equal(t, email, *got.GoogleUserEmail)
	require.NotNil(t, got.GrantedScope)
	require.Equal(t, scope, *got.GrantedScope)
	require.NotNil(t, got.WorkspaceEligible)
	require.True(t, *got.WorkspaceEligible)
	require.Equal(t, []byte("access-cipher-1"), got.AccessTokenEncrypted)
	require.Equal(t, []byte("refresh-cipher-1"), got.RefreshTokenEncrypted)

	// Upsert: same creator_identity_id, different tokens + flipped eligibility.
	notEligible := false
	conn2 := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creator,
		GoogleUserID:          "google-sub-123",
		GoogleUserEmail:       &email,
		GrantedScope:          &scope,
		WorkspaceEligible:     &notEligible,
		AccessTokenEncrypted:  []byte("access-cipher-2"),
		RefreshTokenEncrypted: []byte("refresh-cipher-2"),
		ExpiresAt:             time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second),
	}
	require.NoError(t, db.CreateGoogleMeetConnection(ctx, conn2))
	got2, err := db.GetGoogleMeetConnectionByCreatorIdentity(ctx, creator)
	require.NoError(t, err)
	require.NotNil(t, got2)
	require.Equal(t, []byte("access-cipher-2"), got2.AccessTokenEncrypted)
	require.NotNil(t, got2.WorkspaceEligible)
	require.False(t, *got2.WorkspaceEligible)
	// UPSERT keeps the original row id (existing creator_identity_id), so id should match the original.
	require.Equal(t, conn.ID, got2.ID)

	// UpdateTokens.
	newExpiry := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, db.UpdateGoogleMeetConnectionTokens(ctx, creator, []byte("access-cipher-3"), []byte("refresh-cipher-3"), newExpiry))
	got3, err := db.GetGoogleMeetConnectionByCreatorIdentity(ctx, creator)
	require.NoError(t, err)
	require.Equal(t, []byte("access-cipher-3"), got3.AccessTokenEncrypted)
	require.Equal(t, []byte("refresh-cipher-3"), got3.RefreshTokenEncrypted)
	require.WithinDuration(t, newExpiry, got3.ExpiresAt, time.Second)

	// UpdateTokens on missing creator returns error.
	err = db.UpdateGoogleMeetConnectionTokens(ctx, "missing@example.com", []byte("a"), []byte("b"), newExpiry)
	require.Error(t, err)

	// Delete is idempotent.
	require.NoError(t, db.DeleteGoogleMeetConnectionByCreatorIdentity(ctx, creator))
	got4, err := db.GetGoogleMeetConnectionByCreatorIdentity(ctx, creator)
	require.NoError(t, err)
	require.Nil(t, got4)
	// Second delete on already-missing row succeeds.
	require.NoError(t, db.DeleteGoogleMeetConnectionByCreatorIdentity(ctx, creator))
}
