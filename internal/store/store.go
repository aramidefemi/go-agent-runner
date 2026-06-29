package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite db: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Migrate() error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}

	const schema = `
CREATE TABLE IF NOT EXISTS runs (
    id            TEXT PRIMARY KEY,
    workspace     TEXT,
    job_name      TEXT NOT NULL,
    started_at    TEXT NOT NULL,
    finished_at   TEXT,
    duration_ms   INTEGER,
    exit_code     INTEGER,
    timed_out     INTEGER NOT NULL DEFAULT 0,
    skipped       INTEGER NOT NULL DEFAULT 0,
    skip_reason   TEXT,
    log_path      TEXT,
    summary_path  TEXT
);

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);
`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate sqlite schema: %w", err)
	}
	return nil
}

func (s *Store) InsertRun(run Run) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}

	const query = `
INSERT INTO runs (
    id, workspace, job_name, started_at, finished_at, duration_ms, exit_code,
    timed_out, skipped, skip_reason, log_path, summary_path
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

	_, err := s.db.Exec(
		query,
		run.ID,
		run.Workspace,
		run.JobName,
		formatTime(run.StartedAt),
		formatNullableTime(run.FinishedAt),
		durationToMillis(run.Duration),
		run.ExitCode,
		boolToInt(run.TimedOut),
		boolToInt(run.Skipped),
		run.SkipReason,
		run.LogPath,
		run.SummaryPath,
	)
	if err != nil {
		return fmt.Errorf("insert run %q: %w", run.ID, err)
	}
	return nil
}

func (s *Store) UpdateRun(run Run) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}

	const query = `
UPDATE runs
SET workspace = ?,
    job_name = ?,
    started_at = ?,
    finished_at = ?,
    duration_ms = ?,
    exit_code = ?,
    timed_out = ?,
    skipped = ?,
    skip_reason = ?,
    log_path = ?,
    summary_path = ?
WHERE id = ?
`

	result, err := s.db.Exec(
		query,
		run.Workspace,
		run.JobName,
		formatTime(run.StartedAt),
		formatNullableTime(run.FinishedAt),
		durationToMillis(run.Duration),
		run.ExitCode,
		boolToInt(run.TimedOut),
		boolToInt(run.Skipped),
		run.SkipReason,
		run.LogPath,
		run.SummaryPath,
		run.ID,
	)
	if err != nil {
		return fmt.Errorf("update run %q: %w", run.ID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read update result for run %q: %w", run.ID, err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) ListRuns(limit int) ([]Run, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("store is not initialized")
	}
	if limit <= 0 {
		limit = 50
	}

	const query = `
SELECT id, workspace, job_name, started_at, finished_at, duration_ms, exit_code,
       timed_out, skipped, skip_reason, log_path, summary_path
FROM runs
ORDER BY started_at DESC
LIMIT ?
`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []Run
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run rows: %w", err)
	}
	return runs, nil
}

func (s *Store) GetRun(id string) (*Run, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("store is not initialized")
	}

	const query = `
SELECT id, workspace, job_name, started_at, finished_at, duration_ms, exit_code,
       timed_out, skipped, skip_reason, log_path, summary_path
FROM runs
WHERE id = ?
`

	row := s.db.QueryRow(query, id)
	run, err := scanRun(row)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) SetMeta(key, value string) error {
	if s == nil || s.db == nil {
		return errors.New("store is not initialized")
	}

	const query = `
INSERT INTO meta(key, value)
VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`

	_, err := s.db.Exec(query, key, value)
	if err != nil {
		return fmt.Errorf("set meta %q: %w", key, err)
	}
	return nil
}

func (s *Store) GetMeta(key string) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("store is not initialized")
	}

	const query = `SELECT value FROM meta WHERE key = ?`

	var value string
	if err := s.db.QueryRow(query, key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get meta %q: %w", key, err)
	}
	return value, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(s scanner) (Run, error) {
	var (
		startedAtRaw  string
		finishedAtRaw sql.NullString
		durationMS    sql.NullInt64
		exitCode      sql.NullInt64
		timedOutInt   int
		skippedInt    int
		run           Run
	)

	err := s.Scan(
		&run.ID,
		&run.Workspace,
		&run.JobName,
		&startedAtRaw,
		&finishedAtRaw,
		&durationMS,
		&exitCode,
		&timedOutInt,
		&skippedInt,
		&run.SkipReason,
		&run.LogPath,
		&run.SummaryPath,
	)
	if err != nil {
		return Run{}, err
	}

	startedAt, err := parseTime(startedAtRaw)
	if err != nil {
		return Run{}, fmt.Errorf("parse started_at: %w", err)
	}
	run.StartedAt = startedAt

	if finishedAtRaw.Valid {
		finishedAt, err := parseTime(finishedAtRaw.String)
		if err != nil {
			return Run{}, fmt.Errorf("parse finished_at: %w", err)
		}
		run.FinishedAt = &finishedAt
	}

	if durationMS.Valid {
		run.Duration = time.Duration(durationMS.Int64) * time.Millisecond
	}
	if exitCode.Valid {
		code := int(exitCode.Int64)
		run.ExitCode = &code
	}
	run.TimedOut = intToBool(timedOutInt)
	run.Skipped = intToBool(skippedInt)
	return run, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intToBool(v int) bool {
	return v != 0
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func formatNullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTime(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, raw)
}

func durationToMillis(d time.Duration) int64 {
	return d.Milliseconds()
}
