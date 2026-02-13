package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func TestDeleteSessionChunksBySource(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	session := createTestSession(t, db, "Test Session")
	materialID := uuid.New()

	ins := SessionChunkInsert{
		SessionID:   session.ID,
		SourceType:  "material",
		SourceID:    &materialID,
		ChunkIdx:    0,
		Text:        "chunk text",
		AnchorJSON:  map[string]interface{}{"page": 1},
		ContentHash: "hash-" + uuid.New().String(),
	}
	id1, err := db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)
	ins.ChunkIdx = 1
	ins.ContentHash = "hash-" + uuid.New().String()
	id2, err := db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)

	chunks, err := db.ListSessionChunksBySessionID(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, chunks, 2)

	err = db.DeleteSessionChunksBySource(ctx, session.ID, "material", materialID)
	require.NoError(t, err)

	chunks, err = db.ListSessionChunksBySessionID(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, chunks, 0)

	_ = id1
	_ = id2
}

func TestDeleteSessionChunksBySource_LeavesOtherSources(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	session := createTestSession(t, db, "Test Session")
	materialID := uuid.New()
	otherID := uuid.New()

	ins := SessionChunkInsert{
		SessionID:   session.ID,
		SourceType:  "material",
		SourceID:    &materialID,
		ChunkIdx:    0,
		Text:        "material chunk",
		AnchorJSON:  nil,
		ContentHash: "hash-mat-" + uuid.New().String(),
	}
	_, err := db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)

	ins.SourceType = "transcript"
	ins.SourceID = &otherID
	ins.ChunkIdx = 0
	ins.Text = "transcript chunk"
	ins.ContentHash = "hash-trans-" + uuid.New().String()
	_, err = db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)

	err = db.DeleteSessionChunksBySource(ctx, session.ID, "material", materialID)
	require.NoError(t, err)

	chunks, err := db.ListSessionChunksBySessionID(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Equal(t, "transcript", chunks[0].SourceType)
}
