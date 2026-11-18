package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/thomaslmaat/homelab-monitor/internal/model"
	_ "modernc.org/sqlite"
	"time"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil { return nil, err }
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA synchronous=NORMAL`,
		`CREATE TABLE IF NOT EXISTS host_metrics (id INTEGER PRIMARY KEY AUTOINCREMENT, hostname TEXT NOT NULL, collected_at TEXT NOT NULL, payload TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_host_metrics_host_time ON host_metrics(hostname,collected_at DESC)`,
		`CREATE TABLE IF NOT EXISTS dashboard_users (username TEXT PRIMARY KEY, salt TEXT NOT NULL, password_hash TEXT NOT NULL, must_change_password INTEGER NOT NULL DEFAULT 1)`,
	} {
		if _, err := db.Exec(q); err != nil { db.Close(); return nil, err }
	}
	return &Store{db}, nil
}

func (s *Store) EnsureDashboardAdmin(ctx context.Context, salt, hash string) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO dashboard_users(username,salt,password_hash,must_change_password) VALUES('admin',?,?,1)`, salt, hash)
	return err
}

func (s *Store) DashboardUser(ctx context.Context, username string) (salt, hash string, mustChange bool, err error) {
	var required int
	err = s.db.QueryRowContext(ctx, `SELECT salt,password_hash,must_change_password FROM dashboard_users WHERE username=?`, username).Scan(&salt,&hash,&required)
	mustChange = required != 0
	return
}

func (s *Store) SetDashboardPassword(ctx context.Context, username, salt, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dashboard_users SET salt=?,password_hash=?,must_change_password=0 WHERE username=?`, salt, hash, username)
	return err
}

func (s *Store) Insert(ctx context.Context, m model.HostMetrics) error {
	b, err := json.Marshal(m); if err != nil { return err }
	_, err = s.db.ExecContext(ctx, `INSERT INTO host_metrics(hostname,collected_at,payload) VALUES(?,?,?)`, m.Hostname, m.CollectedAt.UTC().Format(time.RFC3339Nano), string(b))
	return err
}
func (s *Store) Latest(ctx context.Context) ([]model.HostMetrics, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM host_metrics WHERE id IN (SELECT MAX(id) FROM host_metrics GROUP BY hostname) ORDER BY hostname`)
	if err != nil { return nil, err }; defer rows.Close()
	out := make([]model.HostMetrics, 0)
	for rows.Next() { var raw string; if err := rows.Scan(&raw); err != nil { return nil, err }; var m model.HostMetrics; if err := json.Unmarshal([]byte(raw), &m); err != nil { return nil, err }; out = append(out,m) }
	return out, rows.Err()
}
func (s *Store) History(ctx context.Context, hostname string, limit int) ([]model.HostMetrics, error) {
	if limit < 1 || limit > 500 { limit = 120 }
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM host_metrics WHERE hostname=? ORDER BY collected_at DESC LIMIT ?`, hostname, limit)
	if err != nil { return nil, err }; defer rows.Close()
	out := []model.HostMetrics{}
	for rows.Next() { var raw string; if err := rows.Scan(&raw); err != nil { return nil, err }; var m model.HostMetrics; if err := json.Unmarshal([]byte(raw), &m); err != nil { return nil, err }; out = append(out,m) }
	return out, rows.Err()
}
func (s *Store) Prune(ctx context.Context, retention time.Duration) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM host_metrics WHERE collected_at < ?`, time.Now().Add(-retention).UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) Close() error { return s.db.Close() }
