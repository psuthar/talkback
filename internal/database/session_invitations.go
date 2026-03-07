package database

import (
	"context"
)

// DeleteAllSessionInvitations deletes all rows from session_invitations (new email/token schema). Used for admin reset.
func (db *DB) DeleteAllSessionInvitations(ctx context.Context) (int, error) {
	res, err := db.Pool.Exec(ctx, `DELETE FROM session_invitations`)
	if err != nil {
		return 0, err
	}
	return int(res.RowsAffected()), nil
}
