package sessionlog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"tethon/internal/protocol"
)

var ErrNotFound = errors.New("not found")

var ErrNotTerminal = errors.New("session is not terminal")

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

type CreateSessionParams struct {
	CreateRequestID   string
	CreateFingerprint string
	RepoID            string
	Repo              string
	CWD               string
	Branch            string
	BaseRef           string
	Agent             string
	Backend           string
	CLI               string
	Model             string
	RequestedModel    string
	Status            string
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
		`CREATE TABLE IF NOT EXISTS codex_requests (
			session_id TEXT NOT NULL,
			connection_id TEXT NOT NULL,
			request_id TEXT NOT NULL,
			status TEXT NOT NULL,
			payload_json TEXT NOT NULL,
			result_json TEXT,
			created_at TEXT NOT NULL,
			resolved_at TEXT,
			PRIMARY KEY (session_id, connection_id, request_id),
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		)`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	if err := s.addColumnIfMissing(ctx, "sessions", "backend", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "sessions", "cli", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "sessions", "model", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "sessions", "requested_model", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "sessions", "codex_state", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "sessions", "repo_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "sessions", "base_ref", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing(ctx, "sessions", "worktree_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}

	if err := s.initCodexActions(ctx); err != nil {
		return err
	}

	return s.initCreateRequests(ctx)
}

func (s *Store) addColumnIfMissing(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+definition)
	return err
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
		ID:             newID("ses"),
		RepoID:         params.RepoID,
		Repo:           params.Repo,
		CWD:            params.CWD,
		Branch:         params.Branch,
		BaseRef:        params.BaseRef,
		Agent:          params.Agent,
		Backend:        params.Backend,
		CLI:            params.CLI,
		Model:          params.Model,
		RequestedModel: params.RequestedModel,
		Status:         params.Status,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.Session{}, err
	}
	defer tx.Rollback()

	if params.CreateRequestID != "" {
		_, err := scanCreateRequestBinding(tx.QueryRowContext(ctx, createRequestLookup, params.CreateRequestID, params.CreateRequestID))
		if err == nil {
			return protocol.Session{}, ErrCodexActionConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return protocol.Session{}, err
		}
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO sessions (
		id, repo_id, repo, cwd, branch, base_ref, worktree_path,
		agent, backend, cli, model, requested_model, status,
		created_at, updated_at, create_request_id, create_fingerprint
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.RepoID, session.Repo, session.CWD,
		session.Branch, session.BaseRef, session.WorktreePath,
		session.Agent, session.Backend, session.CLI, session.Model,
		session.RequestedModel, session.Status, formatTime(session.CreatedAt),
		formatTime(session.UpdatedAt), params.CreateRequestID, params.CreateFingerprint,
	)
	if err != nil {
		return protocol.Session{}, err
	}

	if params.CreateRequestID != "" {
		if err := bindCreatedSession(ctx, tx, params.CreateRequestID, params.CreateFingerprint, session.ID, "acknowledged"); err != nil {
			return protocol.Session{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return protocol.Session{}, err
	}

	return session, nil
}

// SetSessionWorktree records the worktree path and branch chosen for a session
// once the host daemon has created the worktree.
func (s *Store) SetSessionWorktree(ctx context.Context, sessionID, worktreePath, branch string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET worktree_path = ?, branch = ?, updated_at = ? WHERE id = ?`,
		worktreePath, branch, formatTime(time.Now().UTC()), sessionID)
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

const sessionColumns = `id, repo_id, repo, cwd, branch, base_ref, worktree_path, agent, backend, cli, model, requested_model, status, created_at, updated_at, codex_state`

func (s *Store) GetSession(ctx context.Context, id string) (protocol.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	session, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.Session{}, ErrNotFound
	}
	return session, err
}

// ListSessionSummaries returns all sessions, newest-first, with the last event
// sequence for each session. Sessions with no events report LastSeq as 0.
func (s *Store) ListSessionSummaries(ctx context.Context) ([]protocol.SessionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+prefixedSessionColumns("s")+`, COALESCE(MAX(e.seq), 0) AS last_seq
		FROM sessions s
		LEFT JOIN events e ON e.session_id = s.id
		GROUP BY s.id
		ORDER BY s.created_at DESC, s.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []protocol.SessionSummary
	for rows.Next() {
		summary, err := scanSessionSummary(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	return summaries, rows.Err()
}

// ListSessionsByStatus returns sessions whose status is in the given set,
// ordered oldest-first. With no statuses it returns nothing.
func (s *Store) ListSessionsByStatus(ctx context.Context, statuses ...string) ([]protocol.Session, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, status := range statuses {
		placeholders[i] = "?"
		args[i] = status
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE status IN (`+strings.Join(placeholders, ", ")+`) ORDER BY created_at ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []protocol.Session
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func scanSession(scanner eventScanner) (protocol.Session, error) {
	var session protocol.Session
	var createdAt, updatedAt, codexState string
	if err := scanner.Scan(&session.ID, &session.RepoID, &session.Repo, &session.CWD, &session.Branch, &session.BaseRef, &session.WorktreePath, &session.Agent, &session.Backend, &session.CLI, &session.Model, &session.RequestedModel, &session.Status, &createdAt, &updatedAt, &codexState); err != nil {
		return protocol.Session{}, err
	}
	if codexState != "" {
		if err := json.Unmarshal([]byte(codexState), &session.Codex); err != nil {
			return protocol.Session{}, err
		}
	}
	session.CreatedAt = parseTime(createdAt)
	session.UpdatedAt = parseTime(updatedAt)
	return session, nil
}

func scanSessionSummary(scanner eventScanner) (protocol.SessionSummary, error) {
	var summary protocol.SessionSummary
	var createdAt, updatedAt, worktreePath, codexState string
	if err := scanner.Scan(&summary.ID, &summary.RepoID, &summary.Repo, &summary.CWD, &summary.Branch, &summary.BaseRef, &worktreePath, &summary.Agent, &summary.Backend, &summary.CLI, &summary.Model, &summary.RequestedModel, &summary.Status, &createdAt, &updatedAt, &codexState, &summary.LastSeq); err != nil {
		return protocol.SessionSummary{}, err
	}
	if codexState != "" {
		if err := json.Unmarshal([]byte(codexState), &summary.Codex); err != nil {
			return protocol.SessionSummary{}, err
		}
	}
	summary.CreatedAt = parseTime(createdAt)
	summary.UpdatedAt = parseTime(updatedAt)
	return summary, nil
}

func prefixedSessionColumns(prefix string) string {
	columns := strings.Split(sessionColumns, ", ")
	prefixed := make([]string, len(columns))
	for i, column := range columns {
		prefixed[i] = prefix + "." + column
	}
	return strings.Join(prefixed, ", ")
}

func (s *Store) SetSessionModel(ctx context.Context, sessionID, model string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET model = ?, updated_at = ? WHERE id = ?`, model, formatTime(time.Now().UTC()), sessionID)
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

func (s *Store) DeleteTerminalSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM sessions WHERE id = ?`, sessionID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return err
	}
	if !isTerminalStatus(status) {
		err = ErrNotTerminal
		return err
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM pending_approvals WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM codex_requests WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM codex_actions WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM events WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return err
	}
	err = tx.Commit()
	return err
}

func isTerminalStatus(status string) bool {
	return status == "done" || status == "error" || status == "cancelled"
}

func (s *Store) AppendEvent(ctx context.Context, sessionID, eventType string, payload any) (protocol.Event, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return protocol.Event{}, err
	}
	payloadJSON, err = boundCodexEventJSON(eventType, payloadJSON)
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
