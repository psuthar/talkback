package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// SessionChunkInsert is input for UpsertSessionChunk (avoids importing rag)
type SessionChunkInsert struct {
	SessionID   uuid.UUID
	SourceType  string
	SourceID    *uuid.UUID
	ChunkIdx    int
	Text        string
	AnchorJSON  map[string]interface{}
	ContentHash string
}

// UpsertSessionChunk inserts a chunk or updates on conflict (session_id, content_hash) for idempotency; returns chunk id
func (db *DB) UpsertSessionChunk(ctx context.Context, c SessionChunkInsert) (uuid.UUID, error) {
	id := uuid.New()
	anchorJSON, _ := json.Marshal(c.AnchorJSON)
	query := `
		INSERT INTO session_chunks (id, session_id, source_type, source_id, chunk_idx, text, anchor_json, content_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (session_id, content_hash) DO UPDATE SET updated_at = now()
		RETURNING id
	`
	err := db.Pool.QueryRow(ctx, query,
		id, c.SessionID, c.SourceType, c.SourceID, c.ChunkIdx, c.Text, anchorJSON, c.ContentHash,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("upsert session chunk: %w", err)
	}
	return id, nil
}

// ListSessionChunksBySessionID returns all chunks for a session (for indexing/retrieval)
func (db *DB) ListSessionChunksBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.SessionChunk, error) {
	query := `SELECT id, session_id, source_type, source_id, chunk_idx, text, anchor_json, content_hash, created_at, updated_at
		FROM session_chunks WHERE session_id = $1 ORDER BY source_type, chunk_idx`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session chunks: %w", err)
	}
	defer rows.Close()
	var out []*models.SessionChunk
	for rows.Next() {
		var c models.SessionChunk
		var anchorJSON []byte
		var sourceID *uuid.UUID
		err := rows.Scan(&c.ID, &c.SessionID, &c.SourceType, &sourceID, &c.ChunkIdx, &c.Text, &anchorJSON, &c.ContentHash, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		c.SourceID = sourceID
		if len(anchorJSON) > 0 {
			_ = json.Unmarshal(anchorJSON, &c.AnchorJSON)
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ListSessionChunksBySessionIDAndSource returns indexed chunks for a session filtered by source_type and optionally source_id.
// Results are ordered by chunk_idx. Limit caps response size (default 500, max 2000).
func (db *DB) ListSessionChunksBySessionIDAndSource(ctx context.Context, sessionID uuid.UUID, sourceType string, sourceID *uuid.UUID, limit int) ([]*models.SessionChunk, error) {
	if limit <= 0 {
		limit = 500
	}
	if limit > 2000 {
		limit = 2000
	}
	var (
		query string
		args  []any
	)
	if sourceID != nil {
		query = `SELECT id, session_id, source_type, source_id, chunk_idx, text, anchor_json, content_hash, created_at, updated_at
			FROM session_chunks WHERE session_id = $1 AND source_type = $2 AND source_id = $3
			ORDER BY chunk_idx LIMIT $4`
		args = []any{sessionID, sourceType, *sourceID, limit}
	} else {
		query = `SELECT id, session_id, source_type, source_id, chunk_idx, text, anchor_json, content_hash, created_at, updated_at
			FROM session_chunks WHERE session_id = $1 AND source_type = $2
			ORDER BY chunk_idx LIMIT $3`
		args = []any{sessionID, sourceType, limit}
	}
	rows, err := db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list session chunks by source: %w", err)
	}
	defer rows.Close()
	var out []*models.SessionChunk
	for rows.Next() {
		var c models.SessionChunk
		var anchorJSON []byte
		var sid *uuid.UUID
		err := rows.Scan(&c.ID, &c.SessionID, &c.SourceType, &sid, &c.ChunkIdx, &c.Text, &anchorJSON, &c.ContentHash, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		c.SourceID = sid
		if len(anchorJSON) > 0 {
			_ = json.Unmarshal(anchorJSON, &c.AnchorJSON)
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// InsertChunkEmbedding inserts an embedding for a chunk (embedding stored as JSONB array of floats)
func (db *DB) InsertChunkEmbedding(ctx context.Context, chunkID, sessionID uuid.UUID, model string, embedding []float32) error {
	// pgx encodes []float32 as JSONB when passed as json.Marshal result
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}
	query := `INSERT INTO session_chunk_embeddings (id, chunk_id, session_id, embedding_model, embedding)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)`
	_, err = db.Pool.Exec(ctx, query, chunkID, sessionID, model, embJSON)
	if err != nil {
		return fmt.Errorf("insert chunk embedding: %w", err)
	}
	return nil
}

// ChunkWithEmbedding is a chunk plus its embedding for similarity search
type ChunkWithEmbedding struct {
	Chunk     models.SessionChunk
	Embedding []float32
}

// ListChunksWithEmbeddingsBySessionID returns all chunks with embeddings for a session (for in-app similarity)
func (db *DB) ListChunksWithEmbeddingsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]ChunkWithEmbedding, error) {
	query := `
		SELECT c.id, c.session_id, c.source_type, c.source_id, c.chunk_idx, c.text, c.anchor_json, c.content_hash, c.created_at, c.updated_at,
		       e.embedding
		FROM session_chunks c
		JOIN session_chunk_embeddings e ON e.chunk_id = c.id
		WHERE c.session_id = $1
		ORDER BY c.source_type, c.chunk_idx
	`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chunks with embeddings: %w", err)
	}
	defer rows.Close()
	var out []ChunkWithEmbedding
	for rows.Next() {
		var c models.SessionChunk
		var anchorJSON, embJSON []byte
		var sourceID *uuid.UUID
		err := rows.Scan(&c.ID, &c.SessionID, &c.SourceType, &sourceID, &c.ChunkIdx, &c.Text, &anchorJSON, &c.ContentHash, &c.CreatedAt, &c.UpdatedAt, &embJSON)
		if err != nil {
			return nil, err
		}
		c.SourceID = sourceID
		if len(anchorJSON) > 0 {
			_ = json.Unmarshal(anchorJSON, &c.AnchorJSON)
		}
		var emb []float32
		if len(embJSON) > 0 {
			var f64 []float64
			if json.Unmarshal(embJSON, &f64) == nil {
				emb = make([]float32, len(f64))
				for i, v := range f64 {
					emb[i] = float32(v)
				}
			}
		}
		out = append(out, ChunkWithEmbedding{Chunk: c, Embedding: emb})
	}
	return out, rows.Err()
}

// ListChunksWithEmbeddingsBySessionIDs returns chunks with embeddings for any of the given sessions (SCRUM-63 cross-session search).
func (db *DB) ListChunksWithEmbeddingsBySessionIDs(ctx context.Context, sessionIDs []uuid.UUID) ([]ChunkWithEmbedding, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}
	query := `
		SELECT c.id, c.session_id, c.source_type, c.source_id, c.chunk_idx, c.text, c.anchor_json, c.content_hash, c.created_at, c.updated_at,
		       e.embedding
		FROM session_chunks c
		JOIN session_chunk_embeddings e ON e.chunk_id = c.id
		WHERE c.session_id = ANY($1::uuid[])
		ORDER BY c.session_id, c.source_type, c.chunk_idx
	`
	rows, err := db.Pool.Query(ctx, query, sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("list chunks with embeddings by sessions: %w", err)
	}
	defer rows.Close()
	var out []ChunkWithEmbedding
	for rows.Next() {
		var c models.SessionChunk
		var anchorJSON, embJSON []byte
		var sourceID *uuid.UUID
		err := rows.Scan(&c.ID, &c.SessionID, &c.SourceType, &sourceID, &c.ChunkIdx, &c.Text, &anchorJSON, &c.ContentHash, &c.CreatedAt, &c.UpdatedAt, &embJSON)
		if err != nil {
			return nil, err
		}
		c.SourceID = sourceID
		if len(anchorJSON) > 0 {
			_ = json.Unmarshal(anchorJSON, &c.AnchorJSON)
		}
		var emb []float32
		if len(embJSON) > 0 {
			var f64 []float64
			if json.Unmarshal(embJSON, &f64) == nil {
				emb = make([]float32, len(f64))
				for i, v := range f64 {
					emb[i] = float32(v)
				}
			}
		}
		out = append(out, ChunkWithEmbedding{Chunk: c, Embedding: emb})
	}
	return out, rows.Err()
}

// CountChunksBySessionID returns the number of chunks for a session
func (db *DB) CountChunksBySessionID(ctx context.Context, sessionID uuid.UUID) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM session_chunks WHERE session_id = $1`, sessionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count chunks: %w", err)
	}
	return n, nil
}

// CountEmbeddingsBySessionID returns the number of embeddings for a session
func (db *DB) CountEmbeddingsBySessionID(ctx context.Context, sessionID uuid.UUID) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM session_chunk_embeddings WHERE session_id = $1`, sessionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count embeddings: %w", err)
	}
	return n, nil
}

// DeleteSessionChunksBySessionID removes all chunks (and cascade embeddings) for a session (for full re-index)
func (db *DB) DeleteSessionChunksBySessionID(ctx context.Context, sessionID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM session_chunks WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete session chunks: %w", err)
	}
	return nil
}

// DeleteChunkEmbeddingsBySessionID removes all embeddings for a session (so we can re-embed without duplicating)
func (db *DB) DeleteChunkEmbeddingsBySessionID(ctx context.Context, sessionID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM session_chunk_embeddings WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete chunk embeddings: %w", err)
	}
	return nil
}

// DeleteSessionChunksBySource removes all chunks for a given source (e.g. one material). Embeddings are cascade-deleted.
func (db *DB) DeleteSessionChunksBySource(ctx context.Context, sessionID uuid.UUID, sourceType string, sourceID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM session_chunks WHERE session_id = $1 AND source_type = $2 AND source_id = $3`,
		sessionID, sourceType, sourceID)
	if err != nil {
		return fmt.Errorf("delete session chunks by source: %w", err)
	}
	return nil
}
