// Package test provides shared test infrastructure for TalkBack.
// factory.go contains seed helpers that create minimal, deterministic fixtures
// for use across handler and database test suites.
package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/require"
)

// FactoryDB is the interface the factory needs from a database.DB. Defined narrowly to avoid
// import cycles with internal/database; *database.DB satisfies it automatically.
type FactoryDB interface {
	CreateUser(ctx context.Context, user *models.User) error
	CreateLoginSession(ctx context.Context, session *models.LoginSession) error
	CreateSession(ctx context.Context, session *models.Session) error
	CreateArtifact(ctx context.Context, sessionID uuid.UUID, title string, description *string) (*models.Artifact, error)
	CreateMaterial(ctx context.Context, material *models.Material) error
}

// MakeUser creates and persists a user with the given email and role. Returns the user.
// Use addAdminSessionCookie / addUserSessionCookie / addParticipantSessionCookie in handler
// tests instead — those helpers also create the auth cookie.
func MakeUser(t *testing.T, db FactoryDB, email string, role models.GlobalRole) *models.User {
	t.Helper()
	u := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: displayNameFromEmail(email),
		Status:      models.UserStatusActive,
		GlobalRole:  role,
	}
	require.NoError(t, db.CreateUser(context.Background(), u), "MakeUser: CreateUser")
	return u
}

// MakeSession creates and persists a session with the given title, owned by createdByEmail.
// Pass "" for createdByEmail if the session should have no owner (e.g. anonymous fixture).
func MakeSession(t *testing.T, db FactoryDB, title string, createdByEmail string) *models.Session {
	t.Helper()
	s := &models.Session{
		ID:        uuid.New(),
		Title:     title,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if createdByEmail != "" {
		s.CreatedBy = &createdByEmail
	}
	require.NoError(t, db.CreateSession(context.Background(), s), "MakeSession: CreateSession")
	return s
}

// MakeArtifact creates and persists an artifact under the given session.
func MakeArtifact(t *testing.T, db FactoryDB, sessionID uuid.UUID, title string) *models.Artifact {
	t.Helper()
	artifact, err := db.CreateArtifact(context.Background(), sessionID, title, nil)
	require.NoError(t, err, "MakeArtifact: CreateArtifact")
	return artifact
}

// MakeMaterial creates and persists a material with extracted text already set (text_status=ready),
// linked to the given artifact and session. text is the raw fixture string.
func MakeMaterial(t *testing.T, db FactoryDB, artifactID, sessionID uuid.UUID, text string) *models.Material {
	t.Helper()
	m := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifactID,
		SessionID:     sessionID,
		Kind:          string(models.MaterialKindDocument),
		Filename:      fmt.Sprintf("fixture-%s.txt", artifactID.String()[:8]),
		ContentType:   "text/plain",
		StorageURL:    "./fixture.txt",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: &text,
	}
	require.NoError(t, db.CreateMaterial(context.Background(), m), "MakeMaterial: CreateMaterial")
	return m
}

// displayNameFromEmail derives a display name from an email address (part before @).
func displayNameFromEmail(email string) string {
	for i, c := range email {
		if c == '@' {
			return email[:i]
		}
	}
	return email
}
