package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
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
		`CREATE TABLE IF NOT EXISTS dashboard_users (username TEXT PRIMARY KEY, salt TEXT NOT NULL, password_hash TEXT NOT NULL, must_change_password INTEGER NOT NULL DEFAULT 1, role TEXT NOT NULL DEFAULT 'admin')`,
	} {
		if _, err := db.Exec(q); err != nil { db.Close(); return nil, err }
	}
	if _, err := db.Exec(`ALTER TABLE dashboard_users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin'`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") { db.Close(); return nil, err }
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

type DashboardAccount struct {
	Username string `json:"username"`
	Role string `json:"role"`
	MustChangePassword bool `json:"must_change_password"`
}

func (s *Store) DashboardRole(ctx context.Context, username string) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT role FROM dashboard_users WHERE username=?`, username).Scan(&role)
	return role, err
}

func (s *Store) AdminMustChangePassword(ctx context.Context) (bool, error) {
	var required int
	err := s.db.QueryRowContext(ctx, `SELECT must_change_password FROM dashboard_users WHERE username='admin'`).Scan(&required)
	return required != 0, err
}

func (s *Store) ListDashboardUsers(ctx context.Context) ([]DashboardAccount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT username,role,must_change_password FROM dashboard_users ORDER BY username`)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []DashboardAccount{}
	for rows.Next() {
		var u DashboardAccount
		var required int
		if err := rows.Scan(&u.Username,&u.Role,&required); err != nil { return nil, err }
		u.MustChangePassword = required != 0
		out = append(out,u)
	}
	return out, rows.Err()
}

func (s *Store) CreateDashboardUser(ctx context.Context, username, salt, hash, role string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO dashboard_users(username,salt,password_hash,must_change_password,role) VALUES(?,?,?,1,?)`, username,salt,hash,role)
	return err
}

func (s *Store) UpdateDashboardUserRole(ctx context.Context, username, role string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dashboard_users SET role=? WHERE username=?`, role,username)
	return err
}

func (s *Store) DeleteDashboardUser(ctx context.Context, username string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM dashboard_users WHERE username=? AND username<>'admin'`, username)
	return err
}

func (s *Store) ResetDashboardPassword(ctx context.Context, username, salt, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE dashboard_users SET salt=?,password_hash=?,must_change_password=1 WHERE username=?`, salt,hash,username)
	return err
}
