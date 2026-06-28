// Package db provides database connection, schema management, and config.
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aipmc/u"

	_ "modernc.org/sqlite"
)

// ── Path resolution ───────────────────────────────────────────────────

// FindPath locates the main database file.
func FindPath() (string, error) {
	if dir := os.Getenv("PMAI_HOME"); dir != "" {
		return filepath.Join(dir, "data", "pmai.db"), nil
	}
	if dir := os.Getenv("PLANAI_HOME"); dir != "" {
		return filepath.Join(dir, "data", "pmai.db"), nil
	}
	if dir := os.Getenv("PROJECT_OS_HOME"); dir != "" {
		return filepath.Join(dir, "data", "pmai.db"), nil
	}
	cwd, _ := os.Getwd()
	for dir := cwd; dir != "/" && dir != "."; {
		pmaiDir := filepath.Join(dir, ".pmai")
		if info, err := os.Stat(pmaiDir); err == nil && info.IsDir() {
			return filepath.Join(pmaiDir, "data", "pmai.db"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(cwd, ".pmai", "data", "pmai.db"), nil
}

// RuntimeDir locates the .pmai directory.
func RuntimeDir() (string, error) {
	if dir := os.Getenv("PMAI_HOME"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("PLANAI_HOME"); dir != "" {
		return dir, nil
	}
	cwd, _ := os.Getwd()
	for dir := cwd; dir != "/" && dir != "."; {
		pmaiDir := filepath.Join(dir, ".pmai")
		if info, err := os.Stat(pmaiDir); err == nil && info.IsDir() {
			return pmaiDir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(cwd, ".pmai"), nil
}

// ── Open / Bootstrap ──────────────────────────────────────────────────

// Open opens the main SQLite database.
func Open() (*sql.DB, error) {
	dbPath, err := FindPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("PMAI database not found: %s — run aipmc init first", dbPath)
	}
	d, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	if err := EnsureSchema(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// OpenVectors opens the separate embeddings database.
func OpenVectors() (*sql.DB, error) {
	dir, err := RuntimeDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "data", "pmai_vectors.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}
	d, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	d.Exec("CREATE TABLE IF NOT EXISTS vectors (id TEXT PRIMARY KEY, embedding_json TEXT NOT NULL)")
	return d, nil
}

// Bootstrap creates a new database at the default path.
func Bootstrap() (string, error) {
	dbPath, err := FindPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return "", err
	}
	d, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return "", err
	}
	defer d.Close()
	if err := EnsureSchema(d); err != nil {
		return "", err
	}
	return dbPath, nil
}

// ── Schema ─────────────────────────────────────────────────────────────

// EnsureSchema creates tables and runs migrations.
func EnsureSchema(d *sql.DB) error {
	for _, stmt := range schemaStatements {
		if _, err := d.Exec(stmt); err != nil {
			return fmt.Errorf("schema: %w\nSQL: %s", err, stmt)
		}
	}
	return migrate(d)
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS canon (id INTEGER PRIMARY KEY CHECK (id = 1), updated_at TEXT NOT NULL, product_goal TEXT NOT NULL, engineering_focus TEXT NOT NULL, architecture TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS canon_items (item_type TEXT NOT NULL, position INTEGER NOT NULL, value TEXT NOT NULL, PRIMARY KEY (item_type, position))`,
	`CREATE TABLE IF NOT EXISTS visions (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL, status TEXT NOT NULL, horizon TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS roadmap (id TEXT PRIMARY KEY, vision_id TEXT, title TEXT NOT NULL, target_date TEXT NOT NULL, status TEXT NOT NULL, priority TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, FOREIGN KEY(vision_id) REFERENCES visions(id))`,
	`CREATE TABLE IF NOT EXISTS plans (id TEXT PRIMARY KEY, roadmap_id TEXT, vision_id TEXT, title TEXT NOT NULL, goal TEXT NOT NULL, status TEXT NOT NULL, priority TEXT NOT NULL, scope_json TEXT NOT NULL, risks_json TEXT NOT NULL, assumptions_json TEXT NOT NULL, task_ids_json TEXT NOT NULL, source TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, FOREIGN KEY(roadmap_id) REFERENCES roadmap(id), FOREIGN KEY(vision_id) REFERENCES visions(id))`,
	`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, title TEXT NOT NULL, status TEXT NOT NULL, priority TEXT NOT NULL, phase TEXT NOT NULL, acceptance_json TEXT NOT NULL, related_docs_json TEXT NOT NULL, related_decisions_json TEXT NOT NULL, last_note TEXT NOT NULL, updated_at TEXT NOT NULL, roadmap_id TEXT, plan_id TEXT, created_at TEXT NOT NULL DEFAULT '')`,
	`CREATE TABLE IF NOT EXISTS principles (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL, kind TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS decisions (id TEXT PRIMARY KEY, title TEXT NOT NULL, date TEXT NOT NULL, status TEXT NOT NULL, background TEXT NOT NULL, decision_text TEXT NOT NULL, impact_json TEXT NOT NULL, alternatives_json TEXT NOT NULL, related_tasks_json TEXT NOT NULL, updates_canon INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS ideas (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL, impact TEXT NOT NULL, source TEXT NOT NULL, status TEXT NOT NULL, canon_conflict INTEGER NOT NULL DEFAULT 0, current_summary TEXT NOT NULL DEFAULT '', main_question TEXT NOT NULL DEFAULT '', recommended_next_action TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS idea_comments (id TEXT PRIMARY KEY, idea_id TEXT NOT NULL, author_type TEXT NOT NULL, author_name TEXT NOT NULL, kind TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY(idea_id) REFERENCES ideas(id))`,
	`CREATE TABLE IF NOT EXISTS links (id TEXT PRIMARY KEY, source_type TEXT NOT NULL, source_id TEXT NOT NULL, relation TEXT NOT NULL, target_type TEXT NOT NULL, target_id TEXT NOT NULL, note TEXT NOT NULL, created_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS doc_records (path TEXT PRIMARY KEY, type TEXT NOT NULL, status TEXT NOT NULL, layer TEXT NOT NULL, source_of_truth INTEGER NOT NULL DEFAULT 0, last_reviewed TEXT NOT NULL, superseded_by TEXT)`,
	`CREATE TABLE IF NOT EXISTS daily_notes (note_date TEXT PRIMARY KEY, completed_json TEXT NOT NULL, problems_json TEXT NOT NULL, risks_json TEXT NOT NULL, next_json TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS commits (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '', evidence_summary TEXT NOT NULL DEFAULT '', review_notes TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL, commit_hash TEXT NOT NULL, task_id TEXT, decision_id TEXT, status TEXT NOT NULL, test_status TEXT NOT NULL, review_status TEXT NOT NULL, files_json TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS bugs (id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL, severity TEXT NOT NULL, status TEXT NOT NULL, commit_id TEXT, error TEXT NOT NULL DEFAULT '', files TEXT NOT NULL DEFAULT '', root_cause TEXT NOT NULL DEFAULT '', fix TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL, FOREIGN KEY(commit_id) REFERENCES commits(id))`,
	`CREATE TABLE IF NOT EXISTS task_notes (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, content TEXT NOT NULL, mode TEXT NOT NULL, created_at TEXT NOT NULL, FOREIGN KEY(task_id) REFERENCES tasks(id))`,
	`CREATE TABLE IF NOT EXISTS events (id TEXT PRIMARY KEY, type TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL, consumed_by_agent INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', source TEXT NOT NULL DEFAULT 'manual', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS thread_items (thread_id TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, added_at TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', PRIMARY KEY (thread_id, entity_type, entity_id), FOREIGN KEY(thread_id) REFERENCES threads(id))`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS fts5_index USING fts5(content, entity_type UNINDEXED, entity_id UNINDEXED, title, tokenize='unicode61')`,
	`CREATE TABLE IF NOT EXISTS agent_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'coder', capabilities TEXT NOT NULL DEFAULT '[]', status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS meeting_rooms (id TEXT PRIMARY KEY, title TEXT NOT NULL, topic TEXT NOT NULL, context TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', agent_roles_context TEXT NOT NULL DEFAULT '', auto_arbitrate INTEGER NOT NULL DEFAULT 0, meeting_mode TEXT NOT NULL DEFAULT 'discussion', created_by TEXT NOT NULL, created_at TEXT NOT NULL, closed_at TEXT)`,
	`CREATE TABLE IF NOT EXISTS meeting_turns (id TEXT PRIMARY KEY, room_id TEXT NOT NULL, turn_number INTEGER NOT NULL, speaker_type TEXT NOT NULL, speaker_id TEXT NOT NULL, question TEXT NOT NULL, response TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'waiting', reply_to TEXT NOT NULL DEFAULT '', address_to TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, FOREIGN KEY(room_id) REFERENCES meeting_rooms(id))`,
	`CREATE TABLE IF NOT EXISTS meeting_participants (meeting_id TEXT NOT NULL, agent_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', confirmed_at TEXT NOT NULL, PRIMARY KEY (meeting_id, agent_id), FOREIGN KEY(meeting_id) REFERENCES meeting_rooms(id), FOREIGN KEY(agent_id) REFERENCES agent_profiles(id))`,
	`CREATE TABLE IF NOT EXISTS agent_assignments (id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, task_id TEXT, role TEXT NOT NULL, scope TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'assigned', assigned_by TEXT NOT NULL, assigned_at TEXT NOT NULL, claimed_at TEXT, completed_at TEXT, FOREIGN KEY(agent_id) REFERENCES agent_profiles(id), FOREIGN KEY(task_id) REFERENCES tasks(id))`,
	`CREATE TABLE IF NOT EXISTS audit_log (id TEXT PRIMARY KEY, actor_type TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, detail_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL)`,
}

func migrate(d *sql.DB) error {
	type migration struct {
		table  string
		column string
		sql    string
	}
	migrations := []migration{
		{"tasks", "roadmap_id", "ALTER TABLE tasks ADD COLUMN roadmap_id TEXT"},
		{"tasks", "plan_id", "ALTER TABLE tasks ADD COLUMN plan_id TEXT"},
		{"tasks", "created_at", "ALTER TABLE tasks ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"},
		{"ideas", "current_summary", "ALTER TABLE ideas ADD COLUMN current_summary TEXT NOT NULL DEFAULT ''"},
		{"ideas", "main_question", "ALTER TABLE ideas ADD COLUMN main_question TEXT NOT NULL DEFAULT ''"},
		{"ideas", "recommended_next_action", "ALTER TABLE ideas ADD COLUMN recommended_next_action TEXT NOT NULL DEFAULT ''"},
		{"ideas", "updated_at", "ALTER TABLE ideas ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"},
		{"commits", "evidence_summary", "ALTER TABLE commits ADD COLUMN evidence_summary TEXT NOT NULL DEFAULT ''"},
		{"commits", "review_notes", "ALTER TABLE commits ADD COLUMN review_notes TEXT NOT NULL DEFAULT ''"},
		{"commits", "summary", "ALTER TABLE commits ADD COLUMN summary TEXT NOT NULL DEFAULT ''"},
		{"bugs", "error", "ALTER TABLE bugs ADD COLUMN error TEXT NOT NULL DEFAULT ''"},
		{"bugs", "files", "ALTER TABLE bugs ADD COLUMN files TEXT NOT NULL DEFAULT ''"},
		{"bugs", "root_cause", "ALTER TABLE bugs ADD COLUMN root_cause TEXT NOT NULL DEFAULT ''"},
		{"bugs", "fix", "ALTER TABLE bugs ADD COLUMN fix TEXT NOT NULL DEFAULT ''"},
		{"bugs", "tags", "ALTER TABLE bugs ADD COLUMN tags TEXT NOT NULL DEFAULT ''"},
	}
	for _, m := range migrations {
		if ColumnExists(d, m.table, m.column) {
			continue
		}
		if _, err := d.Exec(m.sql); err != nil {
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration %s.%s: %w", m.table, m.column, err)
			}
		}
	}
	d.Exec(`UPDATE ideas SET current_summary = CASE WHEN current_summary = '' THEN summary ELSE current_summary END, updated_at = CASE WHEN updated_at = '' THEN created_at ELSE updated_at END`)

	if !tableOrVTableExists(d, "audit_log") {
		d.Exec(`CREATE TABLE IF NOT EXISTS audit_log (id TEXT PRIMARY KEY, actor_type TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, detail_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL)`)
	}
	if !ColumnExists(d, "meeting_rooms", "agent_roles_context") {
		d.Exec("ALTER TABLE meeting_rooms ADD COLUMN agent_roles_context TEXT DEFAULT ''")
	}
	if !ColumnExists(d, "meeting_rooms", "auto_arbitrate") {
		d.Exec("ALTER TABLE meeting_rooms ADD COLUMN auto_arbitrate INTEGER DEFAULT 0")
	}
	if !ColumnExists(d, "meeting_rooms", "meeting_mode") {
		d.Exec("ALTER TABLE meeting_rooms ADD COLUMN meeting_mode TEXT DEFAULT 'discussion'")
	}
	if !ColumnExists(d, "meeting_turns", "reply_to") {
		d.Exec("ALTER TABLE meeting_turns ADD COLUMN reply_to TEXT DEFAULT ''")
	}
	if !ColumnExists(d, "meeting_turns", "address_to") {
		d.Exec("ALTER TABLE meeting_turns ADD COLUMN address_to TEXT DEFAULT ''")
	}
	if !ColumnExists(d, "discussion_log", "embedding_json") {
		d.Exec("ALTER TABLE discussion_log ADD COLUMN embedding_json TEXT DEFAULT ''")
	}
	if !ColumnExists(d, "discussion_log", "metadata") {
		d.Exec("ALTER TABLE discussion_log ADD COLUMN metadata TEXT DEFAULT ''")
	}
	if !tableOrVTableExists(d, "discussion_log") {
		d.Exec(`CREATE TABLE IF NOT EXISTS discussion_log (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, created_at TEXT NOT NULL)`)
	}
	if !ColumnExists(d, "discussion_log", "source") {
		d.Exec("ALTER TABLE discussion_log ADD COLUMN source TEXT NOT NULL DEFAULT ''")
	}
	if !ColumnExists(d, "meeting_rooms", "pm_typing") {
		d.Exec("ALTER TABLE meeting_rooms ADD COLUMN pm_typing INTEGER DEFAULT 0")
	}
	if !ColumnExists(d, "meeting_rooms", "pm_last_visit_at") {
		d.Exec("ALTER TABLE meeting_rooms ADD COLUMN pm_last_visit_at TEXT DEFAULT ''")
	}
	if !ColumnExists(d, "meeting_rooms", "plan_id") {
		d.Exec("ALTER TABLE meeting_rooms ADD COLUMN plan_id TEXT DEFAULT ''")
	}
	for _, spec := range []struct{ table, sql string }{
		{"meeting_rooms", `CREATE TABLE IF NOT EXISTS meeting_rooms (id TEXT PRIMARY KEY, title TEXT NOT NULL, topic TEXT NOT NULL, context TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', agent_roles_context TEXT NOT NULL DEFAULT '', auto_arbitrate INTEGER NOT NULL DEFAULT 0, meeting_mode TEXT NOT NULL DEFAULT 'discussion', created_by TEXT NOT NULL, created_at TEXT NOT NULL, closed_at TEXT)`},
		{"meeting_turns", `CREATE TABLE IF NOT EXISTS meeting_turns (id TEXT PRIMARY KEY, room_id TEXT NOT NULL, turn_number INTEGER NOT NULL, speaker_type TEXT NOT NULL, speaker_id TEXT NOT NULL, question TEXT NOT NULL, response TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'waiting', reply_to TEXT NOT NULL DEFAULT '', address_to TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, FOREIGN KEY(room_id) REFERENCES meeting_rooms(id))`},
		{"meeting_participants", `CREATE TABLE IF NOT EXISTS meeting_participants (meeting_id TEXT NOT NULL, agent_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', confirmed_at TEXT NOT NULL, PRIMARY KEY (meeting_id, agent_id), FOREIGN KEY(meeting_id) REFERENCES meeting_rooms(id), FOREIGN KEY(agent_id) REFERENCES agent_profiles(id))`},
		{"agent_assignments", `CREATE TABLE IF NOT EXISTS agent_assignments (id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, task_id TEXT, role TEXT NOT NULL, scope TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'assigned', assigned_by TEXT NOT NULL, assigned_at TEXT NOT NULL, claimed_at TEXT, completed_at TEXT, FOREIGN KEY(agent_id) REFERENCES agent_profiles(id), FOREIGN KEY(task_id) REFERENCES tasks(id))`},
	} {
		if !tableOrVTableExists(d, spec.table) {
			if _, err := d.Exec(spec.sql); err != nil {
				return fmt.Errorf("migration %s: %w", spec.table, err)
			}
		}
	}
	if !tableOrVTableExists(d, "agent_profiles") {
		d.Exec(`CREATE TABLE IF NOT EXISTS agent_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'coder', capabilities TEXT NOT NULL DEFAULT '[]', status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	}
	if !ColumnExists(d, "meeting_participants", "last_seen_turn") {
		d.Exec("ALTER TABLE meeting_participants ADD COLUMN last_seen_turn INTEGER DEFAULT 0")
	}
	if !tableOrVTableExists(d, "fts5_index") {
		d.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS fts5_index USING fts5(content, entity_type UNINDEXED, entity_id UNINDEXED, title, tokenize='unicode61')`)
		RebuildFTS5Index(d)
	}
	if !tableOrVTableExists(d, "session_summaries") {
		d.Exec(`CREATE TABLE IF NOT EXISTS session_summaries (
			session_id TEXT PRIMARY KEY,
			source TEXT NOT NULL DEFAULT '',
			review_json TEXT NOT NULL DEFAULT '{}',
			summary TEXT NOT NULL DEFAULT '',
			intent TEXT NOT NULL DEFAULT '',
			entity_refs TEXT NOT NULL DEFAULT '[]',
			quality_score INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`)
	}
	return nil
}

func tableOrVTableExists(d *sql.DB, name string) bool {
	var count int
	d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = ?", name).Scan(&count)
	return count > 0
}

// ColumnExists checks if a column exists in a table.
func ColumnExists(d *sql.DB, table, column string) bool {
	rows, err := d.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
		if name == column {
			return true
		}
	}
	return false
}

// ── FTS5 helpers ──────────────────────────────────────────────────────

// SyncFTS5Entity inserts or updates an entity in the FTS5 index.
func SyncFTS5Entity(d *sql.DB, entityType, entityID, title, content string) {
	d.Exec("INSERT OR REPLACE INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, ?, ?, ?)",
		content, entityType, entityID, title)
}

// DeleteFTS5Entity removes an entity from the FTS5 index.
func DeleteFTS5Entity(d *sql.DB, entityType, entityID string) {
	d.Exec("DELETE FROM fts5_index WHERE entity_type = ? AND entity_id = ?", entityType, entityID)
}

// RebuildFTS5Index repopulates the FTS5 index from all entity tables.
func RebuildFTS5Index(d *sql.DB) {
	d.Exec("DELETE FROM fts5_index")

	rows, _ := d.Query("SELECT id, title, last_note FROM tasks")
	if rows != nil {
		for rows.Next() {
			var id, title, note string
			rows.Scan(&id, &title, &note)
			d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'task', ?, ?)", title+" "+note, id, title)
		}
		rows.Close()
	}

	rows2, _ := d.Query("SELECT id, title, goal FROM plans")
	if rows2 != nil {
		for rows2.Next() {
			var id, title, goal string
			rows2.Scan(&id, &title, &goal)
			d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'plan', ?, ?)", title+" "+goal, id, title)
		}
		rows2.Close()
	}

	rows3, _ := d.Query("SELECT id, title, summary, evidence_summary, review_notes, files_json FROM commits")
	if rows3 != nil {
		for rows3.Next() {
			var id, title, summary, evidence, reviewNotes, filesJSON string
			rows3.Scan(&id, &title, &summary, &evidence, &reviewNotes, &filesJSON)
			content := title + " " + summary
			if evidence != "" {
				content += " " + evidence
			}
			if reviewNotes != "" {
				content += " " + reviewNotes
			}
			if filesJSON != "" && filesJSON != "[]" {
				content += " " + filesJSON
			}
			d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'commit', ?, ?)", content, id, title)
		}
		rows3.Close()
	}

	rows4, _ := d.Query("SELECT id, title, description, error, root_cause, fix, tags FROM bugs")
	if rows4 != nil {
		for rows4.Next() {
			var id, title, desc, errStr, root, fix, tags string
			rows4.Scan(&id, &title, &desc, &errStr, &root, &fix, &tags)
			d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'bug', ?, ?)", title+" "+desc+" "+errStr+" "+root+" "+fix+" "+tags, id, title)
		}
		rows4.Close()
	}

	rows5, _ := d.Query("SELECT id, title, background, decision_text FROM decisions")
	if rows5 != nil {
		for rows5.Next() {
			var id, title, bg, dt string
			rows5.Scan(&id, &title, &bg, &dt)
			d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'decision', ?, ?)", title+" "+bg+" "+dt, id, title)
		}
		rows5.Close()
	}

	rows6, _ := d.Query("SELECT id, title, summary FROM ideas")
	if rows6 != nil {
		for rows6.Next() {
			var id, title, summary string
			rows6.Scan(&id, &title, &summary)
			d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'idea', ?, ?)", title+" "+summary, id, title)
		}
		rows6.Close()
	}

	rows7, _ := d.Query("SELECT id, title, summary FROM threads")
	if rows7 != nil {
		for rows7.Next() {
			var id, title, summary string
			rows7.Scan(&id, &title, &summary)
			d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'thread', ?, ?)", title+" "+summary, id, title)
		}
		rows7.Close()
	}
}

// ── Config ────────────────────────────────────────────────────────────

// Config holds runtime configuration.
type Config struct {
	WebHost             string `json:"web_host"`
	WebPort             int    `json:"web_port"`
	AIEndpoint          string `json:"ai_endpoint,omitempty"`
	AIEmbeddingEndpoint string `json:"ai_embedding_endpoint,omitempty"`
	AIModel             string `json:"ai_model,omitempty"`
	AIChatModel         string `json:"ai_chat_model,omitempty"`
}

// LoadConfig reads config from environment and config.json.
func LoadConfig() Config {
	cfg := Config{WebHost: "127.0.0.1", WebPort: 8720}
	if v := os.Getenv("AI_ENDPOINT"); v != "" {
		cfg.AIEndpoint = v
	}
	if v := os.Getenv("AI_EMBEDDING_ENDPOINT"); v != "" {
		cfg.AIEmbeddingEndpoint = v
	}
	if v := os.Getenv("AI_MODEL"); v != "" {
		cfg.AIModel = v
	}
	if v := os.Getenv("AI_CHAT_MODEL"); v != "" {
		cfg.AIChatModel = v
	}
	dir, err := RuntimeDir()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return cfg
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) == nil {
		if h, ok := raw["web_host"].(string); ok {
			cfg.WebHost = h
		}
		if p, ok := raw["web_port"].(float64); ok {
			cfg.WebPort = int(p)
		}
		if cfg.AIEndpoint == "" {
			if v, ok := raw["ai_endpoint"].(string); ok {
				cfg.AIEndpoint = v
			}
		}
		if cfg.AIEmbeddingEndpoint == "" {
			if v, ok := raw["ai_embedding_endpoint"].(string); ok {
				cfg.AIEmbeddingEndpoint = v
			}
		}
		if cfg.AIModel == "" {
			if v, ok := raw["ai_model"].(string); ok {
				cfg.AIModel = v
			}
		}
		if cfg.AIChatModel == "" {
			if v, ok := raw["ai_chat_model"].(string); ok {
				cfg.AIChatModel = v
			}
		}
	}
	return cfg
}

// SaveConfig writes config to config.json.
func SaveConfig(cfg Config) error {
	dir, err := RuntimeDir()
	if err != nil {
		os.MkdirAll(dir, 0755)
		dir, _ = RuntimeDir()
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), u.MustMarshal(cfg), 0644)
}

// GlobalConfig holds proxy configuration stored at ~/.aipmc/config.json.
type GlobalConfig struct {
	ProxyPort    int    `json:"proxy_port"`
	UpstreamURL  string `json:"upstream_url"`
	ProxyModel   string `json:"proxy_model"`
	ProxyLogDir  string `json:"proxy_log_dir"`
	AnthropicURL string `json:"anthropic_url"`
}

func globalConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", "config.json")
}

// LoadGlobalConfig reads ~/.aipmc/config.json with env var overrides.
func LoadGlobalConfig() GlobalConfig {
	cfg := GlobalConfig{
		ProxyPort:   19530,
		UpstreamURL: "http://localhost:8080/v1",
	}
	if v := os.Getenv("PROXY_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.ProxyPort)
	}
	if v := os.Getenv("UPSTREAM_URL"); v != "" {
		cfg.UpstreamURL = v
	}
	if v := os.Getenv("UPSTREAM_MODEL"); v != "" {
		cfg.ProxyModel = v
	}
	if v := os.Getenv("ANTHROPIC_URL"); v != "" {
		cfg.AnthropicURL = v
	}
	data, err := os.ReadFile(globalConfigPath())
	if err != nil {
		return cfg
	}
	var raw map[string]any
	if json.Unmarshal(data, &raw) == nil {
		if p, ok := raw["proxy_port"].(float64); ok && cfg.ProxyPort == 19530 {
			cfg.ProxyPort = int(p)
		}
		if v, ok := raw["upstream_url"].(string); ok && cfg.UpstreamURL == "http://localhost:8080/v1" {
			cfg.UpstreamURL = v
		}
		if v, ok := raw["proxy_model"].(string); ok && cfg.ProxyModel == "" {
			cfg.ProxyModel = v
		}
		if v, ok := raw["proxy_log_dir"].(string); ok && cfg.ProxyLogDir == "" {
			cfg.ProxyLogDir = v
		}
		if v, ok := raw["anthropic_url"].(string); ok && cfg.AnthropicURL == "" {
			cfg.AnthropicURL = v
		}
	}
	return cfg
}

// SaveGlobalConfig writes ~/.aipmc/config.json.
func SaveGlobalConfig(cfg GlobalConfig) error {
	path := globalConfigPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, u.MustMarshal(cfg), 0644)
}

// ProjectEntry records a registered project in ~/.aipmc/projects.json.
type ProjectEntry struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	WebPort   int    `json:"web_port"`
	ProxyPort int    `json:"proxy_port"`
}

func projectsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", "projects.json")
}

// LoadProjects reads the project registry from ~/.aipmc/projects.json.
func LoadProjects() map[string]ProjectEntry {
	data, err := os.ReadFile(projectsPath())
	if err != nil {
		return map[string]ProjectEntry{}
	}
	var projects map[string]ProjectEntry
	if json.Unmarshal(data, &projects) != nil {
		return map[string]ProjectEntry{}
	}
	if projects == nil {
		projects = map[string]ProjectEntry{}
	}
	return projects
}

// SaveProject registers a project in ~/.aipmc/projects.json.
func SaveProject(entry ProjectEntry) error {
	projects := LoadProjects()
	projects[entry.Path] = entry
	path := projectsPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, u.MustMarshal(projects), 0644)
}
