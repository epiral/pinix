// Role:    SQLite storage for scheduler execution history
// Depends: database/sql, encoding/json, fmt, os, path/filepath, time, modernc.org/sqlite
// Exports: SchedulerStore, NewSchedulerStore

package daemon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SchedulerStore persists schedule execution history to SQLite.
type SchedulerStore struct {
	db *sql.DB
}

// NewSchedulerStore opens or creates the scheduler SQLite database.
func NewSchedulerStore(dataDir string) (*SchedulerStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create scheduler data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "scheduler.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open scheduler database: %w", err)
	}

	s := &SchedulerStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate scheduler database: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *SchedulerStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SchedulerStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			schedule_id TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			success INTEGER NOT NULL DEFAULT 0,
			output TEXT DEFAULT '',
			error TEXT DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_executions_schedule ON executions(schedule_id, id DESC);
	`)
	return err
}

// RecordExecution inserts an execution record.
func (s *SchedulerStore) RecordExecution(exec ScheduleExecution) error {
	outputStr := ""
	if len(exec.Output) > 0 {
		outputStr = string(exec.Output)
	}
	_, err := s.db.Exec(
		`INSERT INTO executions (schedule_id, started_at, finished_at, duration_ms, success, output, error)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		exec.ScheduleID,
		exec.StartedAt.Format(time.RFC3339),
		exec.FinishedAt.Format(time.RFC3339),
		exec.DurationMs,
		boolToInt(exec.Success),
		outputStr,
		exec.Error,
	)
	return err
}

// ListExecutions returns the most recent executions for a schedule.
func (s *SchedulerStore) ListExecutions(scheduleID string, limit int) ([]ScheduleExecution, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT schedule_id, started_at, finished_at, duration_ms, success, output, error
		 FROM executions WHERE schedule_id = ? ORDER BY id DESC LIMIT ?`,
		scheduleID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var execs []ScheduleExecution
	for rows.Next() {
		var (
			e         ScheduleExecution
			startStr  string
			finishStr string
			success   int
			output    string
		)
		if err := rows.Scan(&e.ScheduleID, &startStr, &finishStr, &e.DurationMs, &success, &output, &e.Error); err != nil {
			return nil, err
		}
		e.StartedAt, _ = time.Parse(time.RFC3339, startStr)
		e.FinishedAt, _ = time.Parse(time.RFC3339, finishStr)
		e.Success = success != 0
		if output != "" {
			e.Output = json.RawMessage(output)
		}
		execs = append(execs, e)
	}
	return execs, rows.Err()
}

// LastExecution returns the most recent execution for a schedule, or nil.
func (s *SchedulerStore) LastExecution(scheduleID string) *ScheduleExecution {
	execs, err := s.ListExecutions(scheduleID, 1)
	if err != nil || len(execs) == 0 {
		return nil
	}
	return &execs[0]
}

// DeleteExecutions removes all execution records for a schedule.
func (s *SchedulerStore) DeleteExecutions(scheduleID string) error {
	_, err := s.db.Exec(`DELETE FROM executions WHERE schedule_id = ?`, scheduleID)
	return err
}

// Cleanup removes old execution records, keeping at most maxPerSchedule per schedule.
func (s *SchedulerStore) Cleanup(maxPerSchedule int) error {
	_, err := s.db.Exec(`
		DELETE FROM executions WHERE id NOT IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY schedule_id ORDER BY id DESC) AS rn
				FROM executions
			) WHERE rn <= ?
		)
	`, maxPerSchedule)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
