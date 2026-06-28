package sessionlog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"tethon/internal/protocol"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

type CreateSessionParams struct {
	Repo   string
	CWD    string
	Branch string
	Agent  string
	Status string
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.init(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			repo TEXT NOT NULL,
			cwd TEXT NOT NULL,
			branch TEXT NOT NULL,
			agent TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			session_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			type TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (session_id, seq),
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,
		`CREATE TABLE IF NOT EXISTS pending_approvals (
			request_id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			status TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			resolved_at TEXT,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateSession(ctx context.Context, params CreateSessionParams) (protocol.Session, error) {
	if params.Agent == "" {
		params.Agent = "fake"
	}
	if params.Status == "" {
		params.Status = "running"
	}
	now := time.Now().UTC()
	session := protocol.Session{
		ID:        newID("ses"),
		Repo:      params.Repo,
		CWD:       params.CWD,
		Branch:    params.Branch,
		Agent:     params.Agent,
		Status:    params.Status,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, repo, cwd, branch, agent, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Repo, session.CWD, session.Branch, session.Agent, session.Status, formatTime(session.CreatedAt), formatTime(session.UpdatedAt))
	if err != nil {
		return protocol.Session{}, err
	}
	return session, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (protocol.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, repo, cwd, branch, agent, status, created_at, updated_at FROM sessions WHERE id = ?`, id)
	var session protocol.Session
	var createdAt, updatedAt string
	if err := row.Scan(&session.ID, &session.Repo, &session.CWD, &session.Branch, &session.Agent, &session.Status, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Session{}, ErrNotFound
		}
		return protocol.Session{}, err
	}
	session.CreatedAt = parseTime(createdAt)
	session.UpdatedAt = parseTime(updatedAt)
	return session, nil
}

func (s *Store) SetSessionStatus(ctx context.Context, sessionID, status string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET status = ?, updated_at = ? WHERE id = ?`, status, formatTime(time.Now().UTC()), sessionID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AppendEvent(ctx context.Context, sessionID, eventType string, payload any) (protocol.Event, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return protocol.Event{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.Event{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE id = ?`, sessionID).Scan(&exists); err != nil {
		return protocol.Event{}, err
	}
	if exists == 0 {
		err = ErrNotFound
		return protocol.Event{}, err
	}

	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM events WHERE session_id = ?`, sessionID).Scan(&seq); err != nil {
		return protocol.Event{}, err
	}

	event := protocol.Event{
		SessionID: sessionID,
		Seq:       seq,
		Type:      eventType,
		CreatedAt: time.Now().UTC(),
		Payload:   json.RawMessage(payloadJSON),
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events (session_id, seq, type, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)`, event.SessionID, event.Seq, event.Type, string(event.Payload), formatTime(event.CreatedAt)); err != nil {
		return protocol.Event{}, err
	}
	if err = tx.Commit(); err != nil {
		return protocol.Event{}, err
	}
	return event, nil
}

func (s *Store) Replay(ctx context.Context, sessionID string, fromSeq int64) ([]protocol.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT session_id, seq, type, payload_json, created_at
		FROM events WHERE session_id = ? AND seq > ? ORDER BY seq ASC`, sessionID, fromSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []protocol.Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *Store) CreatePendingApproval(ctx context.Context, sessionID, requestID string, payload any) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO pending_approvals (request_id, session_id, status, payload_json, created_at)
		VALUES (?, ?, 'pending', ?, ?)`, requestID, sessionID, string(payloadJSON), formatTime(time.Now().UTC()))
	return err
}

func (s *Store) ResolveApproval(ctx context.Context, sessionID, requestID, action string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE pending_approvals SET status = ?, resolved_at = ?
		WHERE session_id = ? AND request_id = ? AND status = 'pending'`, action, formatTime(time.Now().UTC()), sessionID, requestID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type eventScanner interface {
	Scan(dest ...any) error
}

func scanEvent(scanner eventScanner) (protocol.Event, error) {
	var event protocol.Event
	var payload, createdAt string
	if err := scanner.Scan(&event.SessionID, &event.Seq, &event.Type, &payload, &createdAt); err != nil {
		return protocol.Event{}, err
	}
	event.Payload = json.RawMessage(payload)
	event.CreatedAt = parseTime(createdAt)
	return event, nil
}

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}
