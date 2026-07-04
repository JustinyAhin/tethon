package hub

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"tethon/internal/protocol"
)

var errDeviceNotFound = errors.New("device not found")

type DeviceStore struct {
	db  *sql.DB
	now func() time.Time
}

func OpenDeviceStore(ctx context.Context, path string) (*DeviceStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &DeviceStore{db: db, now: time.Now}
	if err := store.init(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *DeviceStore) Close() error {
	return s.db.Close()
}

func (s *DeviceStore) init(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS devices (
			device_id TEXT PRIMARY KEY,
			push_service TEXT NOT NULL,
			push_token TEXT NOT NULL,
			platform TEXT NOT NULL,
			app_id TEXT NOT NULL DEFAULT '',
			app_version TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			disabled_at TEXT
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *DeviceStore) Register(ctx context.Context, req protocol.RegisterDeviceRequest) (protocol.RegisterDeviceResponse, error) {
	now := s.now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO devices (device_id, push_service, push_token, platform, app_id, app_version, created_at, updated_at, disabled_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(device_id) DO UPDATE SET
			push_service = excluded.push_service,
			push_token = excluded.push_token,
			platform = excluded.platform,
			app_id = excluded.app_id,
			app_version = excluded.app_version,
			updated_at = excluded.updated_at,
			disabled_at = NULL`,
		req.DeviceID, req.PushService, req.PushToken, req.Platform, req.AppID, req.AppVersion, formatTime(now), formatTime(now))
	if err != nil {
		return protocol.RegisterDeviceResponse{}, err
	}
	return protocol.RegisterDeviceResponse{
		DeviceID:    req.DeviceID,
		PushService: req.PushService,
		Platform:    req.Platform,
		UpdatedAt:   now,
	}, nil
}

func (s *DeviceStore) Unregister(ctx context.Context, deviceID string) error {
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET disabled_at = ?, updated_at = ? WHERE device_id = ? AND disabled_at IS NULL`,
		formatTime(now), formatTime(now), deviceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return errDeviceNotFound
	}
	return nil
}

func (s *DeviceStore) ExpoPushTokens(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT push_token FROM devices WHERE push_service = 'expo' AND disabled_at IS NULL ORDER BY updated_at DESC, device_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tokens := []string{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

func validateDevice(req protocol.RegisterDeviceRequest) error {
	if strings.TrimSpace(req.DeviceID) == "" {
		return errors.New("device_id is required")
	}
	if strings.Contains(req.DeviceID, "/") {
		return errors.New("device_id cannot contain slash")
	}
	if req.PushService != "expo" {
		return errors.New("push_service must be expo")
	}
	if strings.TrimSpace(req.PushToken) == "" {
		return errors.New("push_token is required")
	}
	switch req.Platform {
	case "ios", "android":
	default:
		return errors.New("platform must be ios or android")
	}
	return nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
