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

func TestListSessionChunksBySessionIDAndSource(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	session := createTestSession(t, db, "Test Session")
	materialID := uuid.New()
	videoID := uuid.New()

	ins := SessionChunkInsert{
		SessionID:   session.ID,
		SourceType:  "material",
		SourceID:    &materialID,
		ChunkIdx:    0,
		Text:        "mat a",
		AnchorJSON:  nil,
		ContentHash: "hash-m0-" + uuid.New().String(),
	}
	_, err := db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)
	ins.ChunkIdx = 1
	ins.Text = "mat b"
	ins.ContentHash = "hash-m1-" + uuid.New().String()
	_, err = db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)

	ins.SourceType = "transcript"
	ins.SourceID = &videoID
	ins.ChunkIdx = 0
	ins.Text = "trans"
	ins.ContentHash = "hash-t0-" + uuid.New().String()
	_, err = db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)

	allMat, err := db.ListSessionChunksBySessionIDAndSource(ctx, session.ID, "material", nil, 100)
	require.NoError(t, err)
	require.Len(t, allMat, 2)

	oneMat, err := db.ListSessionChunksBySessionIDAndSource(ctx, session.ID, "material", &materialID, 100)
	require.NoError(t, err)
	require.Len(t, oneMat, 2)

	trans, err := db.ListSessionChunksBySessionIDAndSource(ctx, session.ID, "transcript", &videoID, 10)
	require.NoError(t, err)
	require.Len(t, trans, 1)
	require.Equal(t, "trans", trans[0].Text)
}

func TestListChunksWithEmbeddingsBySessionIDs_emptySessionList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	out, err := db.ListChunksWithEmbeddingsBySessionIDs(ctx, nil)
	require.NoError(t, err)
	require.Nil(t, out)

	out, err = db.ListChunksWithEmbeddingsBySessionIDs(ctx, []uuid.UUID{})
	require.NoError(t, err)
	require.Nil(t, out)
}

func TestListChunksWithEmbeddingsBySessionIDs_excludesChunksWithoutEmbedding(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	session := createTestSession(t, db, "Embed filter")
	ins := SessionChunkInsert{
		SessionID:   session.ID,
		SourceType:  "material",
		SourceID:    nil,
		ChunkIdx:    0,
		Text:        "no emb yet",
		AnchorJSON:  nil,
		ContentHash: "hash-noemb-" + uuid.New().String(),
	}
	chunkID, err := db.UpsertSessionChunk(ctx, ins)
	require.NoError(t, err)

	out, err := db.ListChunksWithEmbeddingsBySessionIDs(ctx, []uuid.UUID{session.ID})
	require.NoError(t, err)
	require.Len(t, out, 0)

	require.NoError(t, db.InsertChunkEmbedding(ctx, chunkID, session.ID, "unit-test", []float32{1, 0, 0}))
	out, err = db.ListChunksWithEmbeddingsBySessionIDs(ctx, []uuid.UUID{session.ID})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, chunkID, out[0].Chunk.ID)
	require.Len(t, out[0].Embedding, 3)
}

func TestListChunksWithEmbeddingsBySessionIDs_multipleSessions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	s1 := createTestSession(t, db, "S1")
	s2 := createTestSession(t, db, "S2")

	mk := func(sid uuid.UUID, idx int, text, hashPrefix string) uuid.UUID {
		ins := SessionChunkInsert{
			SessionID:   sid,
			SourceType:  "material",
			SourceID:    nil,
			ChunkIdx:    idx,
			Text:        text,
			AnchorJSON:  nil,
			ContentHash: hashPrefix + uuid.New().String(),
		}
		id, err := db.UpsertSessionChunk(ctx, ins)
		require.NoError(t, err)
		require.NoError(t, db.InsertChunkEmbedding(ctx, id, sid, "unit-test", []float32{float32(idx), 1, 0}))
		return id
	}
	id1 := mk(s1.ID, 0, "a", "h1-")
	id2 := mk(s2.ID, 0, "b", "h2-")

	out, err := db.ListChunksWithEmbeddingsBySessionIDs(ctx, []uuid.UUID{s1.ID, s2.ID})
	require.NoError(t, err)
	require.Len(t, out, 2)
	ids := map[uuid.UUID]struct{}{out[0].Chunk.ID: {}, out[1].Chunk.ID: {}}
	_, ok1 := ids[id1]
	_, ok2 := ids[id2]
	require.True(t, ok1)
	require.True(t, ok2)

	out, err = db.ListChunksWithEmbeddingsBySessionIDs(ctx, []uuid.UUID{s1.ID})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, id1, out[0].Chunk.ID)
}
