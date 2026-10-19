package sessionlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tethon/internal/protocol"
)

var ErrCodexActionConflict = errors.New("the transport request ID was already used for different input")

func (s *Store) initCodexActions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS codex_actions (
		session_id TEXT NOT NULL,
		request_id TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		status TEXT NOT NULL,
		response_json TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (session_id, request_id),
		FOREIGN KEY (session_id) REFERENCES sessions(id)
	)`)
	return err
}

// BeginCodexAction claims the ID before any native write. A process crash or
// lost acknowledgement leaves it uncertain, so another process cannot resend it.
func (s *Store) BeginCodexAction(ctx context.Context, sessionID, requestID, fingerprint string) (string, json.RawMessage, error) {
	if requestID == "" || fingerprint == "" {
		return "", nil, errors.New("transport request ID and fingerprint are required")
	}

	now := formatTime(time.Now().UTC())
	inserted, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO codex_actions
		(session_id, request_id, fingerprint, status, created_at, updated_at)
		SELECT ?, ?, ?, 'uncertain', ?, ? WHERE EXISTS (SELECT 1 FROM sessions WHERE id = ?)`,
		sessionID, requestID, fingerprint, now, now, sessionID)
	if err != nil {
		return "", nil, err
	}

	count, err := inserted.RowsAffected()
	if err != nil {
		return "", nil, err
	}
	if count == 1 {
		return "new", nil, nil
	}

	var storedFingerprint, status string
	var response sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT fingerprint, status, response_json FROM codex_actions
		WHERE session_id = ? AND request_id = ?`, sessionID, requestID).Scan(&storedFingerprint, &status, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	if storedFingerprint != fingerprint {
		return "", nil, ErrCodexActionConflict
	}

	return status, json.RawMessage(response.String), nil
}

func (s *Store) CompleteCodexAction(ctx context.Context, sessionID, requestID, fingerprint, status string, response json.RawMessage) error {
	if status != "acknowledged" && status != "rejected" {
		return errors.New("invalid native action receipt state")
	}
	if !json.Valid(response) || len(response) > protocol.MaxCodexPayloadBytes {
		return errors.New("native action receipt must contain bounded JSON")
	}

	updated, err := s.db.ExecContext(ctx, `UPDATE codex_actions SET status = ?, response_json = ?, updated_at = ?
		WHERE session_id = ? AND request_id = ? AND fingerprint = ?
		AND (status = 'uncertain' OR (status = 'acknowledged' AND ? = 'acknowledged'))`,
		status, string(response), formatTime(time.Now().UTC()), sessionID, requestID, fingerprint, status)
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

// ConfirmCodexAction requires an observed native user-message client ID or
// native queue entry. Missing history never proves that an action was rejected.
func (s *Store) ConfirmCodexAction(ctx context.Context, sessionID, requestID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE codex_actions SET status = 'acknowledged', response_json = '{"events":[]}', updated_at = ?
		WHERE session_id = ? AND request_id = ? AND status = 'uncertain'`,
		formatTime(time.Now().UTC()), sessionID, requestID)
	return err
}
