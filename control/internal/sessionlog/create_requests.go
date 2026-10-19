package sessionlog

import (
	"context"
	"database/sql"
	"errors"

	"tethon/internal/protocol"
)

var ErrCreateRequestUncertain = errors.New("the native resume intent is unconfirmed; check the conversation before another attempt")

type createRequestBinding struct {
	sessionID   string
	fingerprint string
	status      string
	message     string
}

const createRequestLookup = `SELECT session_id, fingerprint, status, message FROM create_requests WHERE request_id = ?
	UNION ALL SELECT id, create_fingerprint, 'acknowledged', '' FROM sessions WHERE create_request_id = ?
	LIMIT 1`

func (s *Store) initCreateRequests(ctx context.Context) error {
	for _, column := range []string{"create_request_id", "create_fingerprint"} {
		if err := s.addColumnIfMissing(ctx, "sessions", column, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}

	statements := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS sessions_create_request
			ON sessions(create_request_id) WHERE create_request_id != ''`,
		// Receipts outlive deleted sessions so an old ID cannot execute elsewhere.
		`CREATE TABLE IF NOT EXISTS create_requests (
			request_id TEXT PRIMARY KEY,
			fingerprint TEXT NOT NULL,
			session_id TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('pending', 'acknowledged', 'rejected')),
			message TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	return nil
}

// FindCreatedSession returns the conversation already bound to a create intent.
// A client retry must not start a second engine or send its prompt again.
func (s *Store) FindCreatedSession(ctx context.Context, requestID, fingerprint string) (protocol.Session, bool, error) {
	if requestID == "" || fingerprint == "" {
		return protocol.Session{}, false, errors.New("create request ID and fingerprint are required")
	}

	binding, err := scanCreateRequestBinding(s.db.QueryRowContext(ctx, createRequestLookup, requestID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.Session{}, false, nil
	}
	if err != nil {
		return protocol.Session{}, false, err
	}
	if binding.fingerprint != fingerprint {
		return protocol.Session{}, false, ErrCodexActionConflict
	}
	if err := createRequestOutcome(binding); err != nil {
		return protocol.Session{}, true, err
	}

	session, err := s.GetSession(ctx, binding.sessionID)

	return session, true, err
}

// BindCreatedSession claims a resume intent before any native action. A repeat
// pending claim cannot dispatch again, even after a host process restart.
func (s *Store) BindCreatedSession(ctx context.Context, requestID, fingerprint, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := bindCreatedSession(ctx, tx, requestID, fingerprint, sessionID, "pending"); err != nil {
		return err
	}

	return tx.Commit()
}

func bindCreatedSession(ctx context.Context, tx *sql.Tx, requestID, fingerprint, sessionID, status string) error {
	if requestID == "" || fingerprint == "" || sessionID == "" {
		return errors.New("create request ID, fingerprint, and session ID are required")
	}

	inserted, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO create_requests
		(request_id, fingerprint, session_id, status)
		SELECT ?, ?, ?, CASE WHEN EXISTS (SELECT 1 FROM sessions WHERE id = ? AND create_request_id = ?)
			THEN 'acknowledged' ELSE ? END
		WHERE EXISTS (SELECT 1 FROM sessions WHERE id = ?)
		AND NOT EXISTS (SELECT 1 FROM sessions WHERE create_request_id = ?
			AND (id != ? OR create_fingerprint != ?))`,
		requestID, fingerprint, sessionID, sessionID, requestID, status,
		sessionID, requestID, sessionID, fingerprint)
	if err != nil {
		return err
	}

	count, err := inserted.RowsAffected()
	if err != nil {
		return err
	}
	if count == 1 {
		return nil
	}

	binding, err := scanCreateRequestBinding(tx.QueryRowContext(ctx, createRequestLookup, requestID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if binding.sessionID != sessionID || binding.fingerprint != fingerprint {
		return ErrCodexActionConflict
	}

	return createRequestOutcome(binding)
}

// CompleteCreatedSession records only a confirmed outcome. Uncertain delivery
// leaves the pending receipt in place rather than allowing another dispatch.
func (s *Store) CompleteCreatedSession(ctx context.Context, requestID, fingerprint, sessionID, status, message string) error {
	if status != "acknowledged" && status != "rejected" {
		return errors.New("invalid create request outcome")
	}
	if (status == "acknowledged" && message != "") ||
		(status == "rejected" && message == "") {
		return errors.New("only a rejected create request must include its error message")
	}

	updated, err := s.db.ExecContext(ctx, `UPDATE create_requests SET status = ?, message = ?
		WHERE request_id = ? AND fingerprint = ? AND session_id = ?
		AND (status = 'pending' OR (status = ? AND message = ?))`,
		status, message, requestID, fingerprint, sessionID, status, message)
	if err != nil {
		return err
	}

	count, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrCodexActionConflict
	}

	return nil
}

func scanCreateRequestBinding(row *sql.Row) (createRequestBinding, error) {
	var binding createRequestBinding
	err := row.Scan(&binding.sessionID, &binding.fingerprint, &binding.status, &binding.message)

	return binding, err
}

func createRequestOutcome(binding createRequestBinding) error {
	switch binding.status {
	case "acknowledged":
		return nil
	case "pending":
		return ErrCreateRequestUncertain
	case "rejected":
		return errors.New(binding.message)
	default:
		return errors.New("invalid stored create request outcome")
	}
}
