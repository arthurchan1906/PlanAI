// Package db provides database connection, schema management, and config.
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aipmc/u"

	_ "modernc.org/sqlite"
)

// ── Path resolution ───────────────────────────────────────────────────

// SCHEMA_VERSION is the persistent schema version marker stored in
// SQLite's PRAGMA user_version. Every time the schema or migrations
// change, bump this — connections with user_version >= this skip the
// (expensive, write-lock-acquiring) EnsureSchema DDL entirely.
const SCHEMA_VERSION = 1

// schemaUpToDate reports whether the database at d already has the
// current schema version, so we can skip the DDL pass on hot paths.
func schemaUpToDate(d *sql.DB) (bool, error) {
	var v int
	if err := d.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return false, err
	}
	return v >= SCHEMA_VERSION, nil
}

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

// OpenProject opens the database for a specific project path,
// falling back to cwd-based resolution when projectPath is empty.
func OpenProject(projectPath string) (*sql.DB, error) {
	if projectPath != "" {
		dbPath := filepath.Join(projectPath, ".pmai", "data", "pmai.db")
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("PMAI database not found: %s — run aipmc init first", dbPath)
		}
		d, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
		if err != nil {
			return nil, err
		}
		if err := d.Ping(); err != nil {
			d.Close()
			return nil, err
		}
		if err := ensureSchemaIfNeeded(d); err != nil {
			d.Close()
			return nil, err
		}
		return d, nil
	}
	return Open()
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
	d, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	if err := ensureSchemaIfNeeded(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// ensureSchemaIfNeeded runs the schema DDL only when the database is not
// already at the current SCHEMA_VERSION. On the hot path (every Open) this
// is a single cheap PRAGMA read instead of 50+ DDL statements that each
// take a SQLite write lock — which is what caused multi-agent write-lock
// storms (bug-20260805-134225-4f214f).
func ensureSchemaIfNeeded(d *sql.DB) error {
	upToDate, err := schemaUpToDate(d)
	if err != nil {
		// Can't read schema state (DB possibly locked) — don't gamble on
		// DDL while the file may be held by another writer. If the file
		// exists and Ping succeeded, the schema was initialized at some
		// point; skip and let the actual query surface the lock error.
		return nil
	}
	if upToDate {
		return nil
	}
	return EnsureSchema(d)
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
	d, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	if err := d.Ping(); err != nil {
		d.Close()
		return nil, err
	}
	// vectors is a separate database; skip its DDL on hot paths too.
	upToDate, _ := schemaUpToDate(d)
	if !upToDate {
		if _, err := d.Exec("CREATE TABLE IF NOT EXISTS vectors (id TEXT PRIMARY KEY, embedding_json TEXT NOT NULL)"); err != nil {
			d.Close()
			return nil, err
		}
		d.Exec(fmt.Sprintf("PRAGMA user_version = %d", SCHEMA_VERSION))
	}
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
	d, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return "", err
	}
	defer d.Close()
	// Bootstrap is an explicit init command — always run full DDL.
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
	if err := migrate(d); err != nil {
		return err
	}
	// Mark the schema as current so subsequent connections skip the DDL
	// pass entirely (PRAGMA user_version is persistent per database file).
	if _, err := d.Exec(fmt.Sprintf("PRAGMA user_version = %d", SCHEMA_VERSION)); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	return nil
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
	`CREATE TABLE IF NOT EXISTS events (id TEXT PRIMARY KEY, type TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL, consumed_by_agent INTEGER NOT NULL DEFAULT 0, processed_by_agent INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, title TEXT NOT NULL, summary TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', source TEXT NOT NULL DEFAULT 'manual', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS thread_items (thread_id TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, added_at TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', PRIMARY KEY (thread_id, entity_type, entity_id), FOREIGN KEY(thread_id) REFERENCES threads(id))`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS fts5_index USING fts5(content, entity_type UNINDEXED, entity_id UNINDEXED, title, tokenize='unicode61')`,
	`CREATE TABLE IF NOT EXISTS agent_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'coder', capabilities TEXT NOT NULL DEFAULT '[]', status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS meeting_rooms (id TEXT PRIMARY KEY, title TEXT NOT NULL, topic TEXT NOT NULL, context TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', agent_roles_context TEXT NOT NULL DEFAULT '', auto_arbitrate INTEGER NOT NULL DEFAULT 0, meeting_mode TEXT NOT NULL DEFAULT 'discussion', created_by TEXT NOT NULL, created_at TEXT NOT NULL, closed_at TEXT)`,
	`CREATE TABLE IF NOT EXISTS meeting_turns (id TEXT PRIMARY KEY, room_id TEXT NOT NULL, turn_number INTEGER NOT NULL, speaker_type TEXT NOT NULL, speaker_id TEXT NOT NULL, question TEXT NOT NULL, response TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'waiting', reply_to TEXT NOT NULL DEFAULT '', address_to TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, FOREIGN KEY(room_id) REFERENCES meeting_rooms(id))`,
	`CREATE TABLE IF NOT EXISTS meeting_participants (meeting_id TEXT NOT NULL, agent_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', confirmed_at TEXT NOT NULL, PRIMARY KEY (meeting_id, agent_id), FOREIGN KEY(meeting_id) REFERENCES meeting_rooms(id), FOREIGN KEY(agent_id) REFERENCES agent_profiles(id))`,
	`CREATE TABLE IF NOT EXISTS agent_assignments (id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, task_id TEXT, role TEXT NOT NULL, scope TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'assigned', assigned_by TEXT NOT NULL, assigned_at TEXT NOT NULL, claimed_at TEXT, completed_at TEXT, FOREIGN KEY(agent_id) REFERENCES agent_profiles(id), FOREIGN KEY(task_id) REFERENCES tasks(id))`,
	`CREATE TABLE IF NOT EXISTS audit_log (id TEXT PRIMARY KEY, actor_type TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, detail_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS graph_edges (id TEXT PRIMARY KEY, source_type TEXT NOT NULL, source_id TEXT NOT NULL, edge_type TEXT NOT NULL, target_type TEXT NOT NULL, target_id TEXT NOT NULL, weight REAL NOT NULL DEFAULT 1.0, evidence_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_graph_edges_unique ON graph_edges(source_type, source_id, edge_type, target_type, target_id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_links_unique ON links(source_type, source_id, relation, target_type, target_id)`,
}

func migrate(d *sql.DB) error {
	type migration struct {
		table  string
		column string
		sql    string
	}
	migrations := []migration{
		{"events", "processed_by_agent", "ALTER TABLE events ADD COLUMN processed_by_agent INTEGER NOT NULL DEFAULT 0"},
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
	// 建表必须先于 ALTER：全新库若先 ALTER 后 CREATE，ALTER 因表不存在
	// 失败且错误被吞（8/10 T1 压测发现 discussion_log 缺 metadata 列）。
	for _, spec := range []struct{ table, sql string }{
		{"meeting_rooms", `CREATE TABLE IF NOT EXISTS meeting_rooms (id TEXT PRIMARY KEY, title TEXT NOT NULL, topic TEXT NOT NULL, context TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', agent_roles_context TEXT NOT NULL DEFAULT '', auto_arbitrate INTEGER NOT NULL DEFAULT 0, meeting_mode TEXT NOT NULL DEFAULT 'discussion', created_by TEXT NOT NULL, created_at TEXT NOT NULL, closed_at TEXT)`},
		{"meeting_turns", `CREATE TABLE IF NOT EXISTS meeting_turns (id TEXT PRIMARY KEY, room_id TEXT NOT NULL, turn_number INTEGER NOT NULL, speaker_type TEXT NOT NULL, speaker_id TEXT NOT NULL, question TEXT NOT NULL, response TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'waiting', reply_to TEXT NOT NULL DEFAULT '', address_to TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, FOREIGN KEY(room_id) REFERENCES meeting_rooms(id))`},
		{"discussion_log", `CREATE TABLE IF NOT EXISTS discussion_log (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, created_at TEXT NOT NULL, embedding_json TEXT DEFAULT '', metadata TEXT DEFAULT '', thread_id TEXT DEFAULT '')`},
	} {
		if !tableOrVTableExists(d, spec.table) {
			if _, err := d.Exec(spec.sql); err != nil {
				return fmt.Errorf("migration %s: %w", spec.table, err)
			}
		}
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
	if !ColumnExists(d, "discussion_log", "thread_id") {
		d.Exec("ALTER TABLE discussion_log ADD COLUMN thread_id TEXT DEFAULT ''")
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
// It first repairs a corrupt index (shadow-table state mismatch makes
// DELETE/INSERT fail with "constraint failed" 1555), then backfills every
// searchable entity type — including discussion_log, which incremental
// SyncFTS5Entity calls otherwise cover.
func RebuildFTS5Index(d *sql.DB) {
	if _, err := d.Exec("INSERT INTO fts5_index(fts5_index, rank) VALUES('rebuild', 0)"); err != nil {
		u.LogShared("FTS5", "rebuild repair err=%v", err)
	}
	// Run the repopulation in a single transaction: on a live DB
	// (proxy writing discussion rows), per-statement writes race and fail
	// with SQLITE_BUSY. One write-lock acquisition avoids that entirely.
	tx, err := d.Begin()
	if err != nil {
		u.LogShared("FTS5", "rebuild begin err=%v", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM fts5_index"); err != nil {
		u.LogShared("FTS5", "rebuild delete err=%v", err)
		return
	}

	indexRows(tx, "SELECT id, title, last_note FROM tasks", "task", func(r *sql.Rows) (string, string, string) {
		var id, title, note string
		r.Scan(&id, &title, &note)
		return id, title, title + " " + note
	})
	indexRows(tx, "SELECT id, title, goal FROM plans", "plan", func(r *sql.Rows) (string, string, string) {
		var id, title, goal string
		r.Scan(&id, &title, &goal)
		return id, title, title + " " + goal
	})
	indexRows(tx, "SELECT id, title, summary, evidence_summary, review_notes, files_json FROM commits", "commit", func(r *sql.Rows) (string, string, string) {
		var id, title, summary, evidence, reviewNotes, filesJSON string
		r.Scan(&id, &title, &summary, &evidence, &reviewNotes, &filesJSON)
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
		return id, title, content
	})
	indexRows(tx, "SELECT id, title, description, error, root_cause, fix, tags FROM bugs", "bug", func(r *sql.Rows) (string, string, string) {
		var id, title, desc, errStr, root, fix, tags string
		r.Scan(&id, &title, &desc, &errStr, &root, &fix, &tags)
		return id, title, title + " " + desc + " " + errStr + " " + root + " " + fix + " " + tags
	})
	indexRows(tx, "SELECT id, title, background, decision_text FROM decisions", "decision", func(r *sql.Rows) (string, string, string) {
		var id, title, bg, dt string
		r.Scan(&id, &title, &bg, &dt)
		return id, title, title + " " + bg + " " + dt
	})
	indexRows(tx, "SELECT id, title, summary FROM ideas", "idea", func(r *sql.Rows) (string, string, string) {
		var id, title, summary string
		r.Scan(&id, &title, &summary)
		return id, title, title + " " + summary
	})
	indexRows(tx, "SELECT id, title, summary FROM threads", "thread", func(r *sql.Rows) (string, string, string) {
		var id, title, summary string
		r.Scan(&id, &title, &summary)
		return id, title, title + " " + summary
	})
	indexRows(tx, "SELECT id, title, summary FROM principles", "principle", func(r *sql.Rows) (string, string, string) {
		var id, title, summary string
		r.Scan(&id, &title, &summary)
		return id, title, title + " " + summary
	})
	indexRows(tx, "SELECT id, title, summary FROM visions", "vision", func(r *sql.Rows) (string, string, string) {
		var id, title, summary string
		r.Scan(&id, &title, &summary)
		return id, title, title + " " + summary
	})
	indexRows(tx, "SELECT id, title FROM roadmap", "roadmap", func(r *sql.Rows) (string, string, string) {
		var id, title string
		r.Scan(&id, &title)
		return id, title, title
	})
	indexRows(tx, "SELECT id, name, role FROM agent_profiles", "agent", func(r *sql.Rows) (string, string, string) {
		var id, name, role string
		r.Scan(&id, &name, &role)
		return id, name, name + " " + role
	})
	indexRows(tx, "SELECT id, role, source, content FROM discussion_log", "discussion", func(r *sql.Rows) (string, string, string) {
		var id, role, source, content string
		r.Scan(&id, &role, &source, &content)
		preview := content
		if rr := []rune(content); len(rr) > 80 {
			preview = string(rr[:80])
		}
		return id, "[" + role + "][" + source + "] " + preview, content
	})
	if err := tx.Commit(); err != nil {
		u.LogShared("FTS5", "rebuild commit err=%v", err)
	}
}

// rowQuerier is satisfied by both *sql.DB and *sql.Tx.
type rowQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
	Exec(string, ...any) (sql.Result, error)
}

// indexRows runs query and inserts each row into the FTS5 index.
// Failures are logged instead of silently swallowed.
func indexRows(d rowQuerier, query, entityType string, scan func(*sql.Rows) (id, title, content string)) {
	rows, err := d.Query(query)
	if err != nil {
		u.LogShared("FTS5", "rebuild query err type=%s err=%v", entityType, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		id, title, content := scan(rows)
		if _, err := d.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, ?, ?, ?)", content, entityType, id, title); err != nil {
			u.LogShared("FTS5", "rebuild insert err type=%s id=%s err=%v", entityType, id, err)
		}
	}
}

// ── Config ────────────────────────────────────────────────────────────

// AgentOverride mirrors the key fields of agent profiles for per-project overrides.
// Empty fields are not applied — the global value is kept.
type AgentOverride struct {
	Model           string            `json:"model,omitempty"`
	EffortLevel     string            `json:"effort_level,omitempty"`
	SubAgentModel   string            `json:"sub_agent_model,omitempty"`
	OpusModel       string            `json:"opus_model,omitempty"`
	SonnetModel     string            `json:"sonnet_model,omitempty"`
	HaikuModel      string            `json:"haiku_model,omitempty"`
	SmallFastModel  string            `json:"small_fast_model,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	ExtraEnv        map[string]string `json:"extra_env,omitempty"`
}

// AgentRuntime is the resolved agent configuration after merging global profile + project overrides.
type AgentRuntime struct {
	Model           string
	EffortLevel     string
	SubAgentModel   string
	OpusModel       string
	SonnetModel     string
	HaikuModel      string
	SmallFastModel  string
	ReasoningEffort string
	ExtraEnv        map[string]string
}

// Config holds runtime configuration.
type Config struct {
	WebHost             string                   `json:"web_host"`
	WebPort             int                      `json:"web_port"`
	AIEndpoint          string                   `json:"ai_endpoint,omitempty"`
	AIEmbeddingEndpoint string                   `json:"ai_embedding_endpoint,omitempty"`
	AIModel             string                   `json:"ai_model,omitempty"`
	AIChatModel         string                   `json:"ai_chat_model,omitempty"`
	AIApiKey            string                   `json:"ai_api_key,omitempty"`
	Model               string                   `json:"model,omitempty"`           // per-project default virtual model
	AgentOverrides      map[string]AgentOverride `json:"agent_overrides,omitempty"` // per-project per-agent overrides
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
	if v := os.Getenv("AI_API_KEY"); v != "" {
		cfg.AIApiKey = v
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
		if cfg.AIApiKey == "" {
			if v, ok := raw["ai_api_key"].(string); ok {
				cfg.AIApiKey = v
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

// ClaudeProfile stores Claude Code–specific launch configuration.
type ClaudeProfile struct {
	Model          string            `json:"model"`            // → ANTHROPIC_MODEL
	SubAgentModel  string            `json:"sub_agent_model"`  // → CLAUDE_CODE_SUBAGENT_MODEL
	OpusModel      string            `json:"opus_model"`       // → ANTHROPIC_DEFAULT_OPUS_MODEL
	SonnetModel    string            `json:"sonnet_model"`     // → ANTHROPIC_DEFAULT_SONNET_MODEL
	HaikuModel     string            `json:"haiku_model"`      // → ANTHROPIC_DEFAULT_HAIKU_MODEL
	SmallFastModel string            `json:"small_fast_model"` // → ANTHROPIC_SMALL_FAST_MODEL
	EffortLevel    string            `json:"effort_level"`     // → CLAUDE_CODE_EFFORT_LEVEL
	ExtraEnv       map[string]string `json:"extra_env,omitempty"`
}

// CodexProfile stores Codex CLI–specific launch configuration.
type CodexProfile struct {
	Model           string            `json:"model"`            // → proxy.config.toml model
	ReasoningEffort string            `json:"reasoning_effort"` // → proxy.config.toml model_reasoning_effort
	ExtraEnv        map[string]string `json:"extra_env,omitempty"`
}

// GeminiProfile stores Gemini CLI–specific launch configuration.
type GeminiProfile struct {
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
}

// OpenCodeProfile stores OpenCode–specific launch configuration.
type OpenCodeProfile struct {
	Models   []string          `json:"models"`
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
}

// GlobalConfig holds proxy configuration stored at ~/.aipmc/config.json.
type GlobalConfig struct {
	ProxyPort     int               `json:"proxy_port"`
	ProxyBindAddr string            `json:"proxy_bind_addr,omitempty"`
	UpstreamURL   string            `json:"upstream_url"`
	ProxyModel    string            `json:"proxy_model"` // deprecated: use per-agent profiles
	ProxyLogDir   string            `json:"proxy_log_dir"`
	AnthropicURL  string            `json:"anthropic_url"`
	ExtraEnv      map[string]string `json:"extra_env,omitempty"` // deprecated: use per-agent profiles

	// DefaultModel is the fallback virtual model used when no per-agent model is configured.
	DefaultModel string `json:"default_model,omitempty"`

	Claude   ClaudeProfile   `json:"claude"`
	Codex    CodexProfile    `json:"codex"`
	Gemini   GeminiProfile   `json:"gemini"`
	OpenCode OpenCodeProfile `json:"opencode"`
}

func globalConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aipmc", "config.json")
}

// SyncOpencodeModels writes the given model list into opencode.json's
// provider.aipm.models section, preserving all other keys (MCP, $schema, etc.).
// If models is empty, the aipm provider entry is left untouched.
func SyncOpencodeModels(projectRoot string, models []string) error {
	if projectRoot == "" {
		return nil
	}
	configPath := filepath.Join(projectRoot, "opencode.json")

	cfg := map[string]any{}
	if data, err := os.ReadFile(configPath); err != nil {
		// No existing opencode.json — nothing to sync into
		return nil
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	provRaw, _ := cfg["provider"]
	prov, _ := provRaw.(map[string]any)
	if prov == nil {
		prov = map[string]any{}
	}

	aipmRaw, _ := prov["aipm"]
	aipm, _ := aipmRaw.(map[string]any)
	if aipm == nil {
		aipm = map[string]any{}
	}

	// Always ensure the provider has the required opencode fields
	aipm["name"] = "AIPM Proxy"
	aipm["npm"] = "@ai-sdk/openai-compatible"

	proxyPort := LoadGlobalConfig().ProxyPort
	proxyBaseURL := fmt.Sprintf("http://localhost:%d/v1", proxyPort)
	if optsRaw, ok := aipm["options"]; ok {
		if opts, ok := optsRaw.(map[string]any); ok {
			opts["baseURL"] = proxyBaseURL
		}
	} else {
		aipm["options"] = map[string]any{"baseURL": proxyBaseURL}
	}

	if len(models) == 0 {
		delete(aipm, "models")
	} else {
		m := map[string]any{}
		for _, name := range models {
			m[name] = map[string]any{"name": name}
		}
		aipm["models"] = m
	}
	prov["aipm"] = aipm
	cfg["provider"] = prov

	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath, data, 0644)
}

// LoadGlobalConfig reads ~/.aipmc/config.json with env var overrides.
func LoadGlobalConfig() GlobalConfig {
	cfg := GlobalConfig{
		ProxyPort:   19530,
		UpstreamURL: "http://localhost:8080/v1",
	}
	// Overlay file config first, then env vars take precedence.
	data, err := os.ReadFile(globalConfigPath())
	if err == nil {
		json.Unmarshal(data, &cfg)
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
	if v := os.Getenv("PROXY_BIND_ADDR"); v != "" {
		cfg.ProxyBindAddr = v
	}
	return cfg
}

// EffectiveAgentModel returns the model for an agent, falling back from
// agent profile → DefaultModel → deprecated global ProxyModel → empty string.
func (g GlobalConfig) EffectiveAgentModel(agent string) string {
	var profileModel string
	switch agent {
	case "claude", "claude-code":
		profileModel = g.Claude.Model
	case "codex", "openai-codex":
		profileModel = g.Codex.Model
	case "opencode", "oc":
		if len(g.OpenCode.Models) > 0 {
			profileModel = g.OpenCode.Models[0]
		}
	}
	if profileModel != "" {
		return profileModel
	}
	if g.DefaultModel != "" {
		return g.DefaultModel
	}
	return g.ProxyModel
}

// EffectiveEnv returns the merged env vars for an agent. Global ExtraEnv
// provides defaults; the agent's own ExtraEnv takes precedence.
func (g GlobalConfig) EffectiveEnv(agent string) map[string]string {
	out := map[string]string{}
	for k, v := range g.ExtraEnv {
		out[k] = v
	}
	var agentEnv map[string]string
	switch agent {
	case "claude", "claude-code":
		agentEnv = g.Claude.ExtraEnv
	case "codex", "openai-codex":
		agentEnv = g.Codex.ExtraEnv
	case "gemini", "gemini-cli":
		agentEnv = g.Gemini.ExtraEnv
	case "opencode", "oc":
		agentEnv = g.OpenCode.ExtraEnv
	}
	for k, v := range agentEnv {
		out[k] = v
	}
	return out
}

// SaveGlobalConfig writes ~/.aipmc/config.json.
func SaveGlobalConfig(cfg GlobalConfig) error {
	path := globalConfigPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, u.MustMarshal(cfg), 0644)
}

// ResolveAgentConfig merges the global agent profile with per-project overrides.
// Priority chain: project agent_overrides → agent profile → DefaultModel → error.
// Empty override fields are not applied — the global value is kept.
// All model-name fields should be filled with virtual model names.
func ResolveAgentConfig(agentType string, global GlobalConfig, project Config) (AgentRuntime, error) {
	rt := AgentRuntime{
		ExtraEnv: map[string]string{},
	}

	// Determine base profile from global config
	switch agentType {
	case "claude", "claude-code":
		rt.Model = global.Claude.Model
		rt.EffortLevel = global.Claude.EffortLevel
		rt.SubAgentModel = global.Claude.SubAgentModel
		rt.OpusModel = global.Claude.OpusModel
		rt.SonnetModel = global.Claude.SonnetModel
		rt.HaikuModel = global.Claude.HaikuModel
		rt.SmallFastModel = global.Claude.SmallFastModel
		for k, v := range global.Claude.ExtraEnv {
			rt.ExtraEnv[k] = v
		}
	case "codex", "openai-codex":
		rt.Model = global.Codex.Model
		rt.ReasoningEffort = global.Codex.ReasoningEffort
		for k, v := range global.Codex.ExtraEnv {
			rt.ExtraEnv[k] = v
		}
	case "gemini", "gemini-cli":
		for k, v := range global.Gemini.ExtraEnv {
			rt.ExtraEnv[k] = v
		}
	case "opencode", "oc":
		if len(global.OpenCode.Models) > 0 {
			rt.Model = global.OpenCode.Models[0]
		}
		for k, v := range global.OpenCode.ExtraEnv {
			rt.ExtraEnv[k] = v
		}
	default:
		rt.Model = global.ProxyModel
	}

	// Merge global ExtraEnv as base
	for k, v := range global.ExtraEnv {
		if _, ok := rt.ExtraEnv[k]; !ok {
			rt.ExtraEnv[k] = v
		}
	}

	// Apply project-level overrides (non-empty only)
	if ov, ok := project.AgentOverrides[agentType]; ok {
		if ov.Model != "" {
			rt.Model = ov.Model
		}
		if ov.EffortLevel != "" {
			rt.EffortLevel = ov.EffortLevel
		}
		if ov.SubAgentModel != "" {
			rt.SubAgentModel = ov.SubAgentModel
		}
		if ov.OpusModel != "" {
			rt.OpusModel = ov.OpusModel
		}
		if ov.SonnetModel != "" {
			rt.SonnetModel = ov.SonnetModel
		}
		if ov.HaikuModel != "" {
			rt.HaikuModel = ov.HaikuModel
		}
		if ov.SmallFastModel != "" {
			rt.SmallFastModel = ov.SmallFastModel
		}
		if ov.ReasoningEffort != "" {
			rt.ReasoningEffort = ov.ReasoningEffort
		}
		for k, v := range ov.ExtraEnv {
			rt.ExtraEnv[k] = v
		}
	}

	// Fallback chain for model (highest to lowest priority):
	//   agent_overrides → agent profile → project.model → DefaultModel → ProxyModel
	if rt.Model == "" {
		rt.Model = project.Model
	}
	if rt.Model == "" {
		rt.Model = global.DefaultModel
	}
	if rt.Model == "" {
		rt.Model = global.ProxyModel
	}

	// Model is not strictly required for OpenCode and Gemini
	if rt.Model == "" && (agentType == "claude" || agentType == "claude-code" || agentType == "codex" || agentType == "openai-codex") {
		return rt, fmt.Errorf("no model configured for agent %s", agentType)
	}

	return rt, nil
}

// ProjectEntry records a registered project in ~/.aipmc/projects.json.
type ProjectEntry struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	WebPort      int    `json:"web_port"`
	ProxyPort    int    `json:"proxy_port"`
	LastOpenedAt string `json:"last_opened_at"`
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

// LoadCleanProjects reads the registry and removes entries whose paths no longer
// exist on disk. Returns projects sorted by last_opened_at descending.
func LoadCleanProjects() []ProjectEntry {
	raw := LoadProjects()
	dirty := false
	for path := range raw {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			delete(raw, path)
			dirty = true
		}
	}
	// Convert to slice and sort by last_opened_at descending
	list := make([]ProjectEntry, 0, len(raw))
	for _, e := range raw {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].LastOpenedAt > list[j].LastOpenedAt
	})
	if dirty {
		m := map[string]ProjectEntry{}
		for _, e := range list {
			m[e.Path] = e
		}
		path := projectsPath()
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, u.MustMarshal(m), 0644)
	}
	return list
}

// SaveProject registers a project in ~/.aipmc/projects.json.
func SaveProject(entry ProjectEntry) error {
	projects := LoadProjects()
	projects[entry.Path] = entry
	path := projectsPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, u.MustMarshal(projects), 0644)
}
