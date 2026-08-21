package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS admins(id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, must_change INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS admin_sessions(id_hash BLOB PRIMARY KEY, admin_id TEXT NOT NULL REFERENCES admins(id), csrf_hash BLOB NOT NULL, csrf_token TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS access_sessions(id TEXT PRIMARY KEY, token_hash BLOB NOT NULL UNIQUE, csrf_hash BLOB NOT NULL, csrf_token TEXT NOT NULL, observed_ip TEXT NOT NULL, expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS invites(id TEXT PRIMARY KEY, token_hash BLOB NOT NULL UNIQUE, expires_at INTEGER NOT NULL, used_at INTEGER, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS nodes(id TEXT PRIMARY KEY, name TEXT NOT NULL, endpoint TEXT NOT NULL, public_key BLOB, status TEXT NOT NULL DEFAULT 'offline', enabled INTEGER NOT NULL DEFAULT 1, tls_ready INTEGER NOT NULL DEFAULT 0, observed_ip TEXT NOT NULL DEFAULT '', asn TEXT NOT NULL DEFAULT 'unknown', network TEXT NOT NULL DEFAULT 'unknown', region TEXT NOT NULL DEFAULT 'unknown', city TEXT NOT NULL DEFAULT 'unknown', latitude REAL, longitude REAL, ip_family TEXT NOT NULL DEFAULT 'unknown', last_seen INTEGER, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS join_tokens(id TEXT PRIMARY KEY, token_hash BLOB NOT NULL UNIQUE, node_id TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL, used_at INTEGER, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS tests(id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES access_sessions(id), status TEXT NOT NULL, client_ip TEXT NOT NULL, created_at INTEGER NOT NULL, completed_at INTEGER);
CREATE TABLE IF NOT EXISTS test_node_results(id INTEGER PRIMARY KEY AUTOINCREMENT, test_id TEXT NOT NULL REFERENCES tests(id), node_id TEXT NOT NULL, latency_ms REAL, loss_percent REAL, jitter_ms REAL, download_mbps REAL, upload_mbps REAL, score REAL, status TEXT NOT NULL, UNIQUE(test_id,node_id));
CREATE TABLE IF NOT EXISTS route_hops(id INTEGER PRIMARY KEY AUTOINCREMENT, test_id TEXT NOT NULL REFERENCES tests(id), node_id TEXT NOT NULL, hop INTEGER NOT NULL, ip_anon TEXT, asn TEXT, region TEXT, latency_ms REAL, geo_quality TEXT NOT NULL DEFAULT 'unknown');
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS node_daily_usage(node_id TEXT NOT NULL, day TEXT NOT NULL, download_bytes INTEGER NOT NULL DEFAULT 0, upload_bytes INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(node_id,day));
CREATE TABLE IF NOT EXISTS audit_events(id INTEGER PRIMARY KEY AUTOINCREMENT, actor TEXT NOT NULL, action TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '{}', created_at INTEGER NOT NULL);
INSERT OR IGNORE INTO schema_migrations(version,applied_at) VALUES(1,unixepoch());`

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA synchronous=NORMAL"} {
		if _, err = db.Exec(q); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{DB: db}, nil
}
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) PutSetting(ctx context.Context, k, v string) error {
	_, e := s.DB.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, k, v, time.Now().Unix())
	return e
}
func (s *Store) Setting(ctx context.Context, k, fallback string) string {
	var v string
	if s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, k).Scan(&v) != nil {
		return fallback
	}
	return v
}

var ErrConsumed = errors.New("token invalid, expired, or consumed")

func (s *Store) ConsumeOneTime(ctx context.Context, table string, hash []byte, now time.Time) error {
	if table != "invites" && table != "join_tokens" {
		return errors.New("invalid token table")
	}
	q := `UPDATE ` + table + ` SET used_at=? WHERE token_hash=? AND used_at IS NULL AND expires_at>=?`
	r, e := s.DB.ExecContext(ctx, q, now.Unix(), hash, now.Unix())
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConsumed
	}
	return nil
}
func (s *Store) Cleanup(ctx context.Context, now time.Time) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM admin_sessions WHERE expires_at<?`, `DELETE FROM access_sessions WHERE expires_at<?`} {
		if _, e = tx.ExecContext(ctx, q, now.Unix()); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM tests WHERE created_at<?`, now.Add(-30*24*time.Hour).Unix()); e != nil {
		return e
	}
	return tx.Commit()
}

func Backup(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer out.Close()
	_, e = out.ReadFrom(in)
	if e == nil {
		e = out.Sync()
	}
	return e
}
