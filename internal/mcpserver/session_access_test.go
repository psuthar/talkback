package mcpserver

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

func TestUserMayReadSessionMCP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := &database.DB{} // not consulted for nil-user or global-admin paths
	sid := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	session := &models.Session{ID: sid}

	t.Run("nil user", func(t *testing.T) {
		t.Parallel()
		ok, err := userMayReadSessionMCP(ctx, db, session, nil)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("expected false")
		}
	})

	t.Run("global admin", func(t *testing.T) {
		t.Parallel()
		u := &models.User{GlobalRole: models.GlobalRoleAdmin}
		ok, err := userMayReadSessionMCP(ctx, db, session, u)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected true")
		}
	})
}
