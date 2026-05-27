// Role:    SQLite storage for agent data (agents, topics, runs, messages, events)
// Depends: database/sql, fmt, os, path/filepath, time
// Exports: Store, NewStore

package agent

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages persistent storage for the agent runtime.
type Store struct {
	db *sql.DB
}

// NewStore opens or creates the SQLite database at the given directory.
// The database file is named "agent.db" within the directory.
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "agent.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	llm_provider TEXT DEFAULT '',
	llm_model TEXT DEFAULT '',
	base_url TEXT DEFAULT '',
	api_key TEXT DEFAULT '',
	system_prompt TEXT DEFAULT '',
	temperature REAL DEFAULT 0.3,
	max_tokens INTEGER DEFAULT 8192,
	enable_reasoning INTEGER DEFAULT 0,
	scope TEXT DEFAULT '',
	pinned TEXT DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS topics (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL REFERENCES agents(id),
	title TEXT DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_topics_agent ON topics(agent_id);

CREATE TABLE IF NOT EXISTS runs (
	id TEXT PRIMARY KEY,
	topic_id TEXT NOT NULL REFERENCES topics(id),
	status TEXT DEFAULT 'running',
	created_at TEXT NOT NULL,
	finished_at TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_runs_topic ON runs(topic_id);

CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	topic_id TEXT NOT NULL REFERENCES topics(id),
	run_id TEXT DEFAULT '',
	role TEXT NOT NULL,
	content TEXT DEFAULT '',
	reasoning TEXT DEFAULT '',
	tool_call_id TEXT DEFAULT '',
	tool_name TEXT DEFAULT '',
	tool_args TEXT DEFAULT '',
	usage TEXT DEFAULT '',
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_topic ON messages(topic_id);
CREATE INDEX IF NOT EXISTS idx_messages_run ON messages(run_id);

CREATE TABLE IF NOT EXISTS events (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL,
	topic_id TEXT DEFAULT '',
	type TEXT NOT NULL,
	schedule TEXT NOT NULL,
	timezone TEXT DEFAULT 'UTC',
	prompt TEXT DEFAULT '',
	status TEXT DEFAULT 'active',
	last_run_at TEXT DEFAULT '',
	next_run_at TEXT DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`

// --- Agent CRUD ---

func (s *Store) ListAgents() ([]Agent, error) {
	rows, err := s.db.Query(`SELECT id, name, llm_provider, llm_model, base_url, api_key, system_prompt, temperature, max_tokens, enable_reasoning, scope, pinned, created_at, updated_at FROM agents ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (s *Store) GetAgent(id string) (*Agent, error) {
	row := s.db.QueryRow(`SELECT id, name, llm_provider, llm_model, base_url, api_key, system_prompt, temperature, max_tokens, enable_reasoning, scope, pinned, created_at, updated_at FROM agents WHERE id = ?`, id)
	a, err := scanAgentRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) SaveAgent(a *Agent) error {
	now := time.Now().UTC()
	if a.ID == "" {
		a.ID = NewID("agt")
		a.CreatedAt = now
	}
	a.UpdatedAt = now

	_, err := s.db.Exec(`INSERT INTO agents (id, name, llm_provider, llm_model, base_url, api_key, system_prompt, temperature, max_tokens, enable_reasoning, scope, pinned, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, llm_provider=excluded.llm_provider, llm_model=excluded.llm_model,
			base_url=excluded.base_url, api_key=excluded.api_key, system_prompt=excluded.system_prompt,
			temperature=excluded.temperature, max_tokens=excluded.max_tokens, enable_reasoning=excluded.enable_reasoning,
			scope=excluded.scope, pinned=excluded.pinned, updated_at=excluded.updated_at`,
		a.ID, a.Name, a.LLMProvider, a.LLMModel, a.BaseURL, a.APIKey, a.SystemPrompt,
		a.Temperature, a.MaxTokens, boolToInt(a.EnableReasoning),
		joinStrings(a.Scope), joinStrings(a.Pinned),
		formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
	return err
}

func (s *Store) DeleteAgent(id string) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	return err
}

// --- Topic CRUD ---

func (s *Store) ListTopics(agentID string) ([]Topic, error) {
	rows, err := s.db.Query(`SELECT id, agent_id, title, created_at, updated_at FROM topics WHERE agent_id = ? ORDER BY updated_at DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []Topic
	for rows.Next() {
		t, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

func (s *Store) GetTopic(id string) (*Topic, error) {
	row := s.db.QueryRow(`SELECT id, agent_id, title, created_at, updated_at FROM topics WHERE id = ?`, id)
	t, err := scanTopicRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) SaveTopic(t *Topic) error {
	now := time.Now().UTC()
	if t.ID == "" {
		t.ID = NewID("top")
		t.CreatedAt = now
	}
	t.UpdatedAt = now

	_, err := s.db.Exec(`INSERT INTO topics (id, agent_id, title, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, updated_at=excluded.updated_at`,
		t.ID, t.AgentID, t.Title, formatTime(t.CreatedAt), formatTime(t.UpdatedAt))
	return err
}

func (s *Store) DeleteTopic(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM messages WHERE topic_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM runs WHERE topic_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM topics WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateTopicTitle(id, title string) error {
	_, err := s.db.Exec(`UPDATE topics SET title = ?, updated_at = ? WHERE id = ?`, title, formatTime(time.Now().UTC()), id)
	return err
}

// --- Run CRUD ---

func (s *Store) CreateRun(topicID string) (*Run, error) {
	r := &Run{
		ID:        NewID("run"),
		TopicID:   topicID,
		Status:    RunStatusRunning,
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.Exec(`INSERT INTO runs (id, topic_id, status, created_at) VALUES (?, ?, ?, ?)`,
		r.ID, r.TopicID, r.Status, formatTime(r.CreatedAt))
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Store) FinishRun(id string, status RunStatus) error {
	_, err := s.db.Exec(`UPDATE runs SET status = ?, finished_at = ? WHERE id = ?`,
		status, formatTime(time.Now().UTC()), id)
	return err
}

func (s *Store) GetRun(id string) (*Run, error) {
	row := s.db.QueryRow(`SELECT id, topic_id, status, created_at, finished_at FROM runs WHERE id = ?`, id)
	var r Run
	var finishedAt string
	err := row.Scan(&r.ID, &r.TopicID, &r.Status, &r.CreatedAt, &finishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if finishedAt != "" {
		r.FinishedAt, _ = time.Parse(time.RFC3339, finishedAt)
	}
	return &r, nil
}

func (s *Store) CancelRunningRuns(topicID string) error {
	_, err := s.db.Exec(`UPDATE runs SET status = ?, finished_at = ? WHERE topic_id = ? AND status = ?`,
		RunStatusCancelled, formatTime(time.Now().UTC()), topicID, RunStatusRunning)
	return err
}

// --- Message CRUD ---

func (s *Store) AddMessage(m *Message) error {
	if m.ID == "" {
		m.ID = NewID("msg")
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`INSERT INTO messages (id, topic_id, run_id, role, content, reasoning, tool_call_id, tool_name, tool_args, usage, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.TopicID, m.RunID, m.Role, m.Content, m.Reasoning, m.ToolCallID, m.ToolName, m.ToolArgs, m.Usage, formatTime(m.CreatedAt))
	return err
}

func (s *Store) ListMessages(topicID string) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, topic_id, run_id, role, content, reasoning, tool_call_id, tool_name, tool_args, usage, created_at FROM messages WHERE topic_id = ? ORDER BY created_at`, topicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) ListRunMessages(runID string) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, topic_id, run_id, role, content, reasoning, tool_call_id, tool_name, tool_args, usage, created_at FROM messages WHERE run_id = ? ORDER BY created_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) CountMessages(topicID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE topic_id = ?`, topicID).Scan(&count)
	return count, err
}

// SearchMessages searches messages across all topics for a query string.
func (s *Store) SearchMessages(query string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}
	pattern := "%" + query + "%"
	rows, err := s.db.Query(`SELECT id, topic_id, run_id, role, content, reasoning, tool_call_id, tool_name, tool_args, usage, created_at
		FROM messages WHERE content LIKE ? ORDER BY created_at DESC LIMIT ?`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// --- Event CRUD ---

func (s *Store) ListEvents(agentID string) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id, agent_id, topic_id, type, schedule, timezone, prompt, status, last_run_at, next_run_at, created_at, updated_at
		FROM events WHERE agent_id = ? ORDER BY created_at`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) SaveEvent(e *Event) error {
	now := time.Now().UTC()
	if e.ID == "" {
		e.ID = NewID("evt")
		e.CreatedAt = now
	}
	e.UpdatedAt = now

	_, err := s.db.Exec(`INSERT INTO events (id, agent_id, topic_id, type, schedule, timezone, prompt, status, last_run_at, next_run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			schedule=excluded.schedule, timezone=excluded.timezone, prompt=excluded.prompt,
			status=excluded.status, last_run_at=excluded.last_run_at, next_run_at=excluded.next_run_at,
			updated_at=excluded.updated_at`,
		e.ID, e.AgentID, e.TopicID, e.Type, e.Schedule, e.Timezone, e.Prompt, e.Status,
		formatTime(e.LastRunAt), formatTime(e.NextRunAt),
		formatTime(e.CreatedAt), formatTime(e.UpdatedAt))
	return err
}

func (s *Store) DueEvents(now time.Time) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id, agent_id, topic_id, type, schedule, timezone, prompt, status, last_run_at, next_run_at, created_at, updated_at
		FROM events WHERE status = 'active' AND next_run_at != '' AND next_run_at <= ? ORDER BY next_run_at`, formatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAgent(rows *sql.Rows) (Agent, error) {
	var a Agent
	var enableReasoning int
	var scope, pinned, createdAt, updatedAt string
	err := rows.Scan(&a.ID, &a.Name, &a.LLMProvider, &a.LLMModel, &a.BaseURL, &a.APIKey,
		&a.SystemPrompt, &a.Temperature, &a.MaxTokens, &enableReasoning,
		&scope, &pinned, &createdAt, &updatedAt)
	if err != nil {
		return a, err
	}
	a.EnableReasoning = enableReasoning != 0
	a.Scope = splitStrings(scope)
	a.Pinned = splitStrings(pinned)
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return a, nil
}

func scanAgentRow(row *sql.Row) (Agent, error) {
	var a Agent
	var enableReasoning int
	var scope, pinned, createdAt, updatedAt string
	err := row.Scan(&a.ID, &a.Name, &a.LLMProvider, &a.LLMModel, &a.BaseURL, &a.APIKey,
		&a.SystemPrompt, &a.Temperature, &a.MaxTokens, &enableReasoning,
		&scope, &pinned, &createdAt, &updatedAt)
	if err != nil {
		return a, err
	}
	a.EnableReasoning = enableReasoning != 0
	a.Scope = splitStrings(scope)
	a.Pinned = splitStrings(pinned)
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return a, nil
}

func scanTopic(rows *sql.Rows) (Topic, error) {
	var t Topic
	var createdAt, updatedAt string
	err := rows.Scan(&t.ID, &t.AgentID, &t.Title, &createdAt, &updatedAt)
	if err != nil {
		return t, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}

func scanTopicRow(row *sql.Row) (Topic, error) {
	var t Topic
	var createdAt, updatedAt string
	err := row.Scan(&t.ID, &t.AgentID, &t.Title, &createdAt, &updatedAt)
	if err != nil {
		return t, err
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}

func scanMessage(rows *sql.Rows) (Message, error) {
	var m Message
	var createdAt string
	err := rows.Scan(&m.ID, &m.TopicID, &m.RunID, &m.Role, &m.Content, &m.Reasoning,
		&m.ToolCallID, &m.ToolName, &m.ToolArgs, &m.Usage, &createdAt)
	if err != nil {
		return m, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return m, nil
}

func scanEvent(rows *sql.Rows) (Event, error) {
	var e Event
	var lastRunAt, nextRunAt, createdAt, updatedAt string
	err := rows.Scan(&e.ID, &e.AgentID, &e.TopicID, &e.Type, &e.Schedule, &e.Timezone,
		&e.Prompt, &e.Status, &lastRunAt, &nextRunAt, &createdAt, &updatedAt)
	if err != nil {
		return e, err
	}
	e.LastRunAt, _ = time.Parse(time.RFC3339, lastRunAt)
	e.NextRunAt, _ = time.Parse(time.RFC3339, nextRunAt)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return e, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func joinStrings(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ","
		}
		result += s
	}
	return result
}

func splitStrings(s string) []string {
	if s == "" {
		return nil
	}
	parts := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
