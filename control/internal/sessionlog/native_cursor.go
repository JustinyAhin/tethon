package sessionlog

import "context"

// LastEventSeq identifies the committed delivery prefix for a native snapshot.
func (s *Store) LastEventSeq(ctx context.Context, sessionID string) (int64, error) {
	var seq int64

	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM events WHERE session_id = ?`, sessionID).Scan(&seq)

	return seq, err
}
