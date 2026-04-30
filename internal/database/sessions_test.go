package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSession(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	t.Run("creates session with all fields", func(t *testing.T) {
		session := &models.Session{
			ID:        uuid.New(),
			Title:     "Weekly Review - Jan 2026",
			CreatedBy: stringPtr("Paresh"),
			Status:    models.SessionStatusOpen,
		}

		err := db.CreateSession(ctx, session)
		require.NoError(t, err)
		assert.NotZero(t, session.CreatedAt)
		assert.NotZero(t, session.UpdatedAt)
		assert.Equal(t, models.SessionStatusOpen, session.Status)
	})

	t.Run("creates session with minimal fields", func(t *testing.T) {
		session := &models.Session{
			ID:     uuid.New(),
			Title:  "Design Review",
			Status: models.SessionStatusOpen,
		}

		err := db.CreateSession(ctx, session)
		require.NoError(t, err)
		assert.NotZero(t, session.CreatedAt)
	})

	t.Run("SCRUM-226: inserts a creator membership when created_by matches a user", func(t *testing.T) {
		creatorEmail := "scrum226-creator-" + uuid.New().String() + "@example.com"
		creator := createTestUser(t, db, creatorEmail, models.GlobalRoleCreator)
		session := &models.Session{
			ID:        uuid.New(),
			Title:     "SCRUM-226 Session",
			CreatedBy: &creatorEmail,
			Status:    models.SessionStatusOpen,
		}
		require.NoError(t, db.CreateSession(ctx, session))

		m, err := db.GetSessionMembership(ctx, session.ID, creator.ID)
		require.NoError(t, err)
		require.NotNil(t, m, "creator must have a session_memberships row after CreateSession")
		assert.Equal(t, "creator", m.Role, "creator membership role must be 'creator'")
	})

	t.Run("SCRUM-226: does not insert a membership when created_by has no matching user", func(t *testing.T) {
		// Imported sessions whose creator does not yet have a users row should
		// still be created — just without the membership invariant. The legacy
		// email-match access path keeps them functional.
		orphanEmail := "orphan-" + uuid.New().String() + "@example.com"
		session := &models.Session{
			ID:        uuid.New(),
			Title:     "Orphan Creator Session",
			CreatedBy: &orphanEmail,
			Status:    models.SessionStatusOpen,
		}
		require.NoError(t, db.CreateSession(ctx, session))

		// No memberships should exist for this session.
		members, err := db.GetSessionMemberships(ctx, session.ID)
		require.NoError(t, err)
		assert.Empty(t, members, "orphan-creator session must have no memberships")
	})
}

func TestGetSession(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create session
	session := &models.Session{
		ID:     uuid.New(),
		Title:  "Test Session",
		Status: models.SessionStatusOpen,
	}
	err := db.CreateSession(ctx, session)
	require.NoError(t, err)

	t.Run("returns session by id", func(t *testing.T) {
		retrieved, err := db.GetSession(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, session.ID, retrieved.ID)
		assert.Equal(t, session.Title, retrieved.Title)
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := db.GetSession(ctx, nonExistentID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session not found")
	})
}

func TestDeleteSession(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	t.Run("deletes session and removes all session data", func(t *testing.T) {
		session := createTestSession(t, db, "To Delete")
		err := db.DeleteFileArtifactsBySessionID(ctx, session.ID)
		require.NoError(t, err)
		err = db.DeleteSession(ctx, session.ID)
		require.NoError(t, err)
		_, err = db.GetSession(ctx, session.ID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns error when deleting non-existent session", func(t *testing.T) {
		err := db.DeleteSession(ctx, uuid.New())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "session not found")
	})
}

func TestDeleteFileArtifactsBySessionID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	t.Run("deletes all file_artifacts for session", func(t *testing.T) {
		session := createTestSession(t, db, "Session With File Artifacts")
		fn := "zoom.mp4"
		fa := &models.FileArtifact{
			ID:              uuid.New(),
			SessionID:       &session.ID,
			Kind:            models.FileArtifactKindVideo,
			Filename:        &fn,
			ContentType:     "video/mp4",
			StorageProvider: "r2",
			StorageBucket:   "bucket",
			StorageKey:      "sessions/" + session.ID.String() + "/v.mp4",
			Status:          models.FileArtifactStatusReady,
		}
		err := db.CreateFileArtifact(ctx, fa)
		require.NoError(t, err)
		err = db.DeleteFileArtifactsBySessionID(ctx, session.ID)
		require.NoError(t, err)
		got, err := db.GetFileArtifactByID(ctx, fa.ID)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestGetSessionsByArtifactID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create sessions
	session := createTestSession(t, db, "Test Session")
	session2 := createTestSession(t, db, "Other Session")
	
	// Create artifacts
	_, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)
	_, err = db.CreateArtifact(ctx, session.ID, "Test Artifact 2", nil)
	require.NoError(t, err)
	artifact2, err := db.CreateArtifact(ctx, session2.ID, "Other Artifact", nil)
	require.NoError(t, err)

	t.Run("returns empty list for session with no artifacts", func(t *testing.T) {
		emptySession := createTestSession(t, db, "Empty Session")
		artifacts, err := db.GetArtifactsBySessionID(ctx, emptySession.ID)
		require.NoError(t, err)
		assert.Empty(t, artifacts)
	})

	t.Run("returns all artifacts for session", func(t *testing.T) {
		// Create more artifacts for session
		artifact3, err := db.CreateArtifact(ctx, session.ID, "Artifact 3", nil)
		require.NoError(t, err)

		artifacts, err := db.GetArtifactsBySessionID(ctx, session.ID)
		require.NoError(t, err)
		assert.Len(t, artifacts, 3)
		// Should be ordered by created_at DESC (most recent first)
		assert.Equal(t, artifact3.ID, artifacts[0].ID)
	})

	t.Run("does not return artifacts for other sessions", func(t *testing.T) {
		artifacts, err := db.GetArtifactsBySessionID(ctx, session2.ID)
		require.NoError(t, err)
		assert.Len(t, artifacts, 1)
		assert.Equal(t, artifact2.ID, artifacts[0].ID)
	})
}

func TestUpsertSessionParticipant(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create session
	session := createTestSession(t, db, "Test Session")

	t.Run("creates new participant", func(t *testing.T) {
		participant, err := db.UpsertSessionParticipant(ctx, session.ID, "anonymous")
		require.NoError(t, err)
		assert.Equal(t, session.ID, participant.SessionID)
		assert.Equal(t, "anonymous", participant.ParticipantRef)
		assert.Equal(t, float32(0), participant.WatchProgress)
		assert.NotZero(t, participant.JoinedAt)
		assert.NotZero(t, participant.LastSeenAt)
	})

	t.Run("updates existing participant", func(t *testing.T) {
		// First join
		participant1, err := db.UpsertSessionParticipant(ctx, session.ID, "user1")
		require.NoError(t, err)
		firstJoinedAt := participant1.JoinedAt
		firstLastSeenAt := participant1.LastSeenAt

		// Join again (should update last_seen_at)
		participant2, err := db.UpsertSessionParticipant(ctx, session.ID, "user1")
		require.NoError(t, err)
		assert.Equal(t, participant1.ID, participant2.ID) // Same participant
		assert.Equal(t, firstJoinedAt, participant2.JoinedAt) // Joined_at should not change
		assert.True(t, participant2.LastSeenAt.After(firstLastSeenAt)) // last_seen_at should update
	})
}

func TestCreateSessionEvent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create session
	session := createTestSession(t, db, "Test Session")

	t.Run("creates play event", func(t *testing.T) {
		event := &models.SessionEvent{
			ID:              uuid.New(),
			SessionID:       session.ID,
			ParticipantRef:  stringPtr("anonymous"),
			EventType:       models.SessionEventTypePlay,
			VideoTimeSeconds: intPtr(120),
			Payload:         map[string]interface{}{"speed": 1.0},
		}

		err := db.CreateSessionEvent(ctx, event)
		require.NoError(t, err)
		assert.NotZero(t, event.CreatedAt)
	})

	t.Run("creates seek event", func(t *testing.T) {
		event := &models.SessionEvent{
			ID:              uuid.New(),
			SessionID:       session.ID,
			ParticipantRef:  stringPtr("anonymous"),
			EventType:       models.SessionEventTypeSeek,
			VideoTimeSeconds: intPtr(300),
			Payload:         map[string]interface{}{},
		}

		err := db.CreateSessionEvent(ctx, event)
		require.NoError(t, err)
	})

	t.Run("creates join event", func(t *testing.T) {
		event := &models.SessionEvent{
			ID:              uuid.New(),
			SessionID:       session.ID,
			ParticipantRef:  stringPtr("user1"),
			EventType:       models.SessionEventTypeJoin,
			Payload:         map[string]interface{}{},
		}

		err := db.CreateSessionEvent(ctx, event)
		require.NoError(t, err)
	})
}

func TestGetQuestionsBySessionID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create session
	session := createTestSession(t, db, "Test Session")

	t.Run("returns empty list for session with no questions", func(t *testing.T) {
		questions, answers, err := db.GetQuestionsBySessionID(ctx, session.ID, 20)
		require.NoError(t, err)
		assert.Empty(t, questions)
		assert.Empty(t, answers)
	})

	t.Run("returns questions with answers for session", func(t *testing.T) {
		// Create an artifact for the question
		artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
		require.NoError(t, err)
		
		// Create session-scoped question
		question := &models.Question{
			ID:             uuid.New(),
			ArtifactID:    artifact.ID,
			SessionID:      session.ID, // SessionID is now required (non-pointer)
			QuestionText:   "What is this about?",
			QuestionSource: models.QuestionSourceText,
		}
		err = db.CreateQuestion(ctx, question)
		require.NoError(t, err)

		// Create answer
		answer := &models.Answer{
			ID:           uuid.New(),
			QuestionID:   question.ID,
			AnswerText:   "This is about testing",
			AnswerStatus: models.AnswerStatusAnswered,
			Confidence:   0.8,
			Citations:    []models.Citation{},
		}
		err = db.CreateAnswer(ctx, answer)
		require.NoError(t, err)

		// Get questions
		questions, answers, err := db.GetQuestionsBySessionID(ctx, session.ID, 20)
		require.NoError(t, err)
		assert.Len(t, questions, 1)
		assert.Len(t, answers, 1)
		assert.Equal(t, question.ID, questions[0].ID)
		assert.Equal(t, answer.ID, answers[0].ID)
		assert.Equal(t, session.ID, questions[0].SessionID)
	})

	t.Run("does not return questions for other sessions", func(t *testing.T) {
		// Create another session and question
		otherSession := createTestSession(t, db, "Other Session")
		otherArtifact, err := db.CreateArtifact(ctx, otherSession.ID, "Other Artifact", nil)
		require.NoError(t, err)

		otherQuestion := &models.Question{
			ID:             uuid.New(),
			ArtifactID:    otherArtifact.ID,
			SessionID:      otherSession.ID,
			QuestionText:   "Other session question?",
			QuestionSource: models.QuestionSourceText,
		}
		err = db.CreateQuestion(ctx, otherQuestion)
		require.NoError(t, err)

		// Get questions for original session - should not include other session's question
		questions, _, err := db.GetQuestionsBySessionID(ctx, session.ID, 20)
		require.NoError(t, err)
		// Should only have the previous session question, not the other session's question
		assert.Len(t, questions, 1)
		assert.NotEqual(t, otherQuestion.ID, questions[0].ID)
	})

	t.Run("returns questions in thread order with parent_question_id", func(t *testing.T) {
		threadSession := createTestSession(t, db, "Thread Session")
		artifact, err := db.CreateArtifact(ctx, threadSession.ID, "Thread Artifact", nil)
		require.NoError(t, err)

		rootQ := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      threadSession.ID,
			QuestionText:   "Root question?",
			QuestionSource: models.QuestionSourceText,
		}
		err = db.CreateQuestion(ctx, rootQ)
		require.NoError(t, err)

		replyQ := &models.Question{
			ID:                uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        threadSession.ID,
			ParentQuestionID: &rootQ.ID,
			QuestionText:     "Follow-up question?",
			QuestionSource:   models.QuestionSourceText,
		}
		err = db.CreateQuestion(ctx, replyQ)
		require.NoError(t, err)

		questions, answers, err := db.GetQuestionsBySessionID(ctx, threadSession.ID, 20)
		require.NoError(t, err)
		require.Len(t, questions, 2, "should return root and reply")
		assert.Len(t, answers, 0, "no answers in this test")

		// Thread order: root first (COALESCE(parent_id, id) = root.ID), then reply
		assert.Nil(t, questions[0].ParentQuestionID, "first should be root")
		assert.Equal(t, rootQ.ID, questions[0].ID)
		require.NotNil(t, questions[1].ParentQuestionID, "second should be reply with parent set")
		assert.Equal(t, rootQ.ID, *questions[1].ParentQuestionID)
		assert.Equal(t, replyQ.ID, questions[1].ID)
	})
}

func TestUpdateSessionStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	session := createTestSession(t, db, "Session to Close")
	assert.Equal(t, models.SessionStatusOpen, session.Status)

	t.Run("updates session status to closed", func(t *testing.T) {
		err := db.UpdateSessionStatus(ctx, session.ID, models.SessionStatusClosed)
		require.NoError(t, err)

		updatedSession, err := db.GetSession(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, models.SessionStatusClosed, updatedSession.Status)
		assert.True(t, updatedSession.UpdatedAt.After(session.CreatedAt))
	})

	t.Run("updates session status back to open", func(t *testing.T) {
		err := db.UpdateSessionStatus(ctx, session.ID, models.SessionStatusOpen)
		require.NoError(t, err)

		updatedSession, err := db.GetSession(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, models.SessionStatusOpen, updatedSession.Status)
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		nonExistentID := uuid.New()
		err := db.UpdateSessionStatus(ctx, nonExistentID, models.SessionStatusClosed)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "session not found")
	})
}

func TestGetSessionTimeline(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	session := createTestSession(t, db, "Timeline Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Timeline Artifact", nil)
	require.NoError(t, err)

	t.Run("returns empty timeline for session with no questions", func(t *testing.T) {
		questions, answers, err := db.GetSessionTimeline(ctx, session.ID, 50)
		require.NoError(t, err)
		assert.Empty(t, questions)
		assert.Empty(t, answers)
	})

	t.Run("returns timeline with questions and answers in chronological order", func(t *testing.T) {
		// Create questions and answers with different timestamps
		now := time.Now()

		q1 := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      session.ID,
			QuestionText:   "First question?",
			QuestionSource: models.QuestionSourceText,
			CreatedAt:      now.Add(-5 * time.Minute),
			VideoTimeSeconds: intPtr(60),
		}
		err := db.CreateQuestion(ctx, q1)
		require.NoError(t, err)

		a1 := &models.Answer{
			ID:           uuid.New(),
			QuestionID:   q1.ID,
			AnswerText:   "First answer",
			AnswerStatus: models.AnswerStatusAnswered,
			Confidence:   0.8,
			Citations:    []models.Citation{},
			CreatedAt:    now.Add(-4 * time.Minute),
		}
		err = db.CreateAnswer(ctx, a1)
		require.NoError(t, err)

		q2 := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      session.ID,
			QuestionText:   "Second question?",
			QuestionSource: models.QuestionSourceText,
			CreatedAt:      now.Add(-3 * time.Minute),
			VideoTimeSeconds: intPtr(120),
		}
		err = db.CreateQuestion(ctx, q2)
		require.NoError(t, err)

		a2 := &models.Answer{
			ID:           uuid.New(),
			QuestionID:   q2.ID,
			AnswerText:   "Second answer",
			AnswerStatus: models.AnswerStatusAnswered,
			Confidence:   0.9,
			Citations:    []models.Citation{},
			CreatedAt:    now.Add(-2 * time.Minute),
		}
		err = db.CreateAnswer(ctx, a2)
		require.NoError(t, err)

		// Get timeline
		questions, answers, err := db.GetSessionTimeline(ctx, session.ID, 50)
		require.NoError(t, err)
		assert.Len(t, questions, 2)
		assert.Len(t, answers, 2)

		// Verify chronological order (oldest first)
		assert.Equal(t, q1.ID, questions[0].ID)
		assert.Equal(t, "First question?", questions[0].QuestionText)
		assert.NotNil(t, questions[0].VideoTimeSeconds)
		assert.Equal(t, 60, *questions[0].VideoTimeSeconds)

		assert.Equal(t, q2.ID, questions[1].ID)
		assert.Equal(t, "Second question?", questions[1].QuestionText)
		assert.NotNil(t, questions[1].VideoTimeSeconds)
		assert.Equal(t, 120, *questions[1].VideoTimeSeconds)

		// Verify answers are matched
		assert.Equal(t, q1.ID, answers[0].QuestionID)
		assert.Equal(t, "First answer", answers[0].AnswerText)

		assert.Equal(t, q2.ID, answers[1].QuestionID)
		assert.Equal(t, "Second answer", answers[1].AnswerText)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		// Create a few more questions to test limit
		for i := 0; i < 5; i++ {
			q := &models.Question{
				ID:             uuid.New(),
				ArtifactID:     artifact.ID,
				SessionID:      session.ID,
				QuestionText:   fmt.Sprintf("Question %d?", i),
				QuestionSource: models.QuestionSourceText,
				CreatedAt:      time.Now(),
			}
			err := db.CreateQuestion(ctx, q)
			require.NoError(t, err)
		}

		// Get timeline with limit of 2
		questions, answers, err := db.GetSessionTimeline(ctx, session.ID, 2)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(questions), 2)
		assert.LessOrEqual(t, len(answers), 2)
	})

	t.Run("does not return questions from other sessions", func(t *testing.T) {
		// Create another session with questions
		otherSession := createTestSession(t, db, "Other Session")
		otherArtifact, err := db.CreateArtifact(ctx, otherSession.ID, "Other Artifact", nil)
		require.NoError(t, err)

		otherQuestion := &models.Question{
			ID:             uuid.New(),
			ArtifactID:     otherArtifact.ID,
			SessionID:      otherSession.ID,
			QuestionText:   "Other session question?",
			QuestionSource: models.QuestionSourceText,
			CreatedAt:      time.Now(),
		}
		err = db.CreateQuestion(ctx, otherQuestion)
		require.NoError(t, err)

		// Get timeline for original session
		questions, _, err := db.GetSessionTimeline(ctx, session.ID, 50)
		require.NoError(t, err)

		// Verify no questions from other session are included
		for _, q := range questions {
			assert.Equal(t, session.ID, q.SessionID)
			assert.NotEqual(t, otherQuestion.ID, q.ID)
		}
	})
}

func intPtr(i int) *int {
	return &i
}
