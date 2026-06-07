package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// ---- DB path resolution ----

func findDBPath() (string, error) {
	if dir := os.Getenv("PMAI_HOME"); dir != "" {
		return filepath.Join(dir, "data", "pmai.db"), nil
	}
	if dir := os.Getenv("PLANAI_HOME"); dir != "" {
		return filepath.Join(dir, "data", "pmai.db"), nil
	}
	if dir := os.Getenv("PROJECT_OS_HOME"); dir != "" {
		return filepath.Join(dir, "data", "pmai.db"), nil
	}
	// Walk up from cwd looking for .pmai/
	cwd, _ := os.Getwd()
	for dir := cwd; dir != "/" && dir != "."; {
		pmaiDir := filepath.Join(dir, ".pmai")
		if info, err := os.Stat(pmaiDir); err == nil && info.IsDir() {
			return filepath.Join(pmaiDir, "data", "pmai.db"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root (e.g. D: on Windows)
		}
		dir = parent
	}
	// Fallback to cwd/.pmai/
	return filepath.Join(cwd, ".pmai", "data", "pmai.db"), nil
}

func openDB() (*sql.DB, error) {
	dbPath, err := findDBPath()
	if err != nil { return nil, err }
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("PMAI database not found: %s — run aipmc init first", dbPath)
	}
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=MEMORY&_synchronous=NORMAL")
	if err != nil { return nil, err }
	if err := db.Ping(); err != nil { db.Close(); return nil, err }
	if err := ensureSchema(db); err != nil { db.Close(); return nil, err }
	return db, nil
}

// openVectorsDB opens the separate embeddings database.
// Vectors are large float arrays that grow quickly — keeping them
// separate keeps the main DB lean and easy to backup/restore.
func openVectorsDB() (*sql.DB, error) {
	dir, err := findRuntimeDir()
	if err != nil { return nil, err }
	dbPath := filepath.Join(dir, "data", "pmai_vectors.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil { return nil, err }
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=MEMORY&_synchronous=NORMAL")
	if err != nil { return nil, err }
	if err := db.Ping(); err != nil { db.Close(); return nil, err }
	db.Exec("CREATE TABLE IF NOT EXISTS vectors (id TEXT PRIMARY KEY, embedding_json TEXT NOT NULL)")
	return db, nil
}

func bootstrapDB() (string, error) {
	dbPath, err := findDBPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=MEMORY&_synchronous=NORMAL")
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := ensureSchema(db); err != nil {
		return "", err
	}
	return dbPath, nil
}

// ---- Schema ----

func ensureSchema(db *sql.DB) error {
	for _, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("schema: %w\nSQL: %s", err, stmt)
		}
	}
	return migrate(db)
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS canon (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		updated_at TEXT NOT NULL,
		product_goal TEXT NOT NULL,
		engineering_focus TEXT NOT NULL,
		architecture TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS canon_items (
		item_type TEXT NOT NULL,
		position INTEGER NOT NULL,
		value TEXT NOT NULL,
		PRIMARY KEY (item_type, position)
	)`,
	`CREATE TABLE IF NOT EXISTS visions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		summary TEXT NOT NULL,
		status TEXT NOT NULL,
		horizon TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS roadmap (
		id TEXT PRIMARY KEY,
		vision_id TEXT,
		title TEXT NOT NULL,
		target_date TEXT NOT NULL,
		status TEXT NOT NULL,
		priority TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(vision_id) REFERENCES visions(id)
	)`,
	`CREATE TABLE IF NOT EXISTS plans (
		id TEXT PRIMARY KEY,
		roadmap_id TEXT,
		vision_id TEXT,
		title TEXT NOT NULL,
		goal TEXT NOT NULL,
		status TEXT NOT NULL,
		priority TEXT NOT NULL,
		scope_json TEXT NOT NULL,
		risks_json TEXT NOT NULL,
		assumptions_json TEXT NOT NULL,
		task_ids_json TEXT NOT NULL,
		source TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(roadmap_id) REFERENCES roadmap(id),
		FOREIGN KEY(vision_id) REFERENCES visions(id)
	)`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		status TEXT NOT NULL,
		priority TEXT NOT NULL,
		phase TEXT NOT NULL,
		acceptance_json TEXT NOT NULL,
		related_docs_json TEXT NOT NULL,
		related_decisions_json TEXT NOT NULL,
		last_note TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		roadmap_id TEXT,
		plan_id TEXT,
		created_at TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS principles (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		summary TEXT NOT NULL,
		kind TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS decisions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		date TEXT NOT NULL,
		status TEXT NOT NULL,
		background TEXT NOT NULL,
		decision_text TEXT NOT NULL,
		impact_json TEXT NOT NULL,
		alternatives_json TEXT NOT NULL,
		related_tasks_json TEXT NOT NULL,
		updates_canon INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS ideas (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		summary TEXT NOT NULL,
		impact TEXT NOT NULL,
		source TEXT NOT NULL,
		status TEXT NOT NULL,
		canon_conflict INTEGER NOT NULL DEFAULT 0,
		current_summary TEXT NOT NULL DEFAULT '',
		main_question TEXT NOT NULL DEFAULT '',
		recommended_next_action TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS idea_comments (
		id TEXT PRIMARY KEY,
		idea_id TEXT NOT NULL,
		author_type TEXT NOT NULL,
		author_name TEXT NOT NULL,
		kind TEXT NOT NULL,
		content TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY(idea_id) REFERENCES ideas(id)
	)`,
	`CREATE TABLE IF NOT EXISTS links (
		id TEXT PRIMARY KEY,
		source_type TEXT NOT NULL,
		source_id TEXT NOT NULL,
		relation TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT NOT NULL,
		note TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS doc_records (
		path TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		status TEXT NOT NULL,
		layer TEXT NOT NULL,
		source_of_truth INTEGER NOT NULL DEFAULT 0,
		last_reviewed TEXT NOT NULL,
		superseded_by TEXT
	)`,
	`CREATE TABLE IF NOT EXISTS daily_notes (
		note_date TEXT PRIMARY KEY,
		completed_json TEXT NOT NULL,
		problems_json TEXT NOT NULL,
		risks_json TEXT NOT NULL,
		next_json TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS commits (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		summary TEXT NOT NULL DEFAULT '',
		evidence_summary TEXT NOT NULL DEFAULT '',
		review_notes TEXT NOT NULL DEFAULT '',
		branch TEXT NOT NULL,
		commit_hash TEXT NOT NULL,
		task_id TEXT,
		decision_id TEXT,
		status TEXT NOT NULL,
		test_status TEXT NOT NULL,
		review_status TEXT NOT NULL,
		files_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS bugs (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		severity TEXT NOT NULL,
		status TEXT NOT NULL,
		commit_id TEXT,
		error TEXT NOT NULL DEFAULT '',
		files TEXT NOT NULL DEFAULT '',
		root_cause TEXT NOT NULL DEFAULT '',
		fix TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(commit_id) REFERENCES commits(id)
	)`,
	`CREATE TABLE IF NOT EXISTS task_notes (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		content TEXT NOT NULL,
		mode TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY(task_id) REFERENCES tasks(id)
	)`,
	`CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		summary TEXT NOT NULL,
		created_at TEXT NOT NULL,
		consumed_by_agent INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		summary TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',
		source TEXT NOT NULL DEFAULT 'manual',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS thread_items (
		thread_id TEXT NOT NULL,
		entity_type TEXT NOT NULL,
		entity_id TEXT NOT NULL,
		added_at TEXT NOT NULL,
		note TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (thread_id, entity_type, entity_id),
		FOREIGN KEY(thread_id) REFERENCES threads(id)
	)`,
	// Note: feedback is stored on remote server (see feedback.go),
	// not in the local SQLite database. Compatible with Python pmai.

	// FTS5 full-text search index — BM25 ranking, CJK-aware via unicode61 tokenizer.
	`CREATE VIRTUAL TABLE IF NOT EXISTS fts5_index USING fts5(
		content,
		entity_type UNINDEXED,
		entity_id UNINDEXED,
		title,
		tokenize='unicode61'
	)`,

		// Agent profiles — registered code agents with capabilities
		`CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'coder',
			capabilities TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,

		// Meeting rooms — human-driven discussion rounds
		`CREATE TABLE IF NOT EXISTS meeting_rooms (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			topic TEXT NOT NULL,
			context TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			agent_roles_context TEXT NOT NULL DEFAULT '',
			auto_arbitrate INTEGER NOT NULL DEFAULT 0,
			meeting_mode TEXT NOT NULL DEFAULT 'discussion',
			created_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			closed_at TEXT
		)`,

		// Meeting turns — PM calls on agent, agent responds
		`CREATE TABLE IF NOT EXISTS meeting_turns (
			id TEXT PRIMARY KEY,
			room_id TEXT NOT NULL,
			turn_number INTEGER NOT NULL,
			speaker_type TEXT NOT NULL,
			speaker_id TEXT NOT NULL,
			question TEXT NOT NULL,
			response TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'waiting',
			reply_to TEXT NOT NULL DEFAULT '',
			address_to TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY(room_id) REFERENCES meeting_rooms(id)
		)`,

		// Meeting participants — agents that confirmed attendance
		`CREATE TABLE IF NOT EXISTS meeting_participants (
			meeting_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			confirmed_at TEXT NOT NULL,
			PRIMARY KEY (meeting_id, agent_id),
			FOREIGN KEY(meeting_id) REFERENCES meeting_rooms(id),
			FOREIGN KEY(agent_id) REFERENCES agent_profiles(id)
		)`,

		// Agent assignments — PM assigns roles to agents
		`CREATE TABLE IF NOT EXISTS agent_assignments (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			task_id TEXT,
			role TEXT NOT NULL,
			scope TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'assigned',
			assigned_by TEXT NOT NULL,
			assigned_at TEXT NOT NULL,
			claimed_at TEXT,
			completed_at TEXT,
			FOREIGN KEY(agent_id) REFERENCES agent_profiles(id),
			FOREIGN KEY(task_id) REFERENCES tasks(id)
		)`,

		// Audit log — records key agent/human operations
		`CREATE TABLE IF NOT EXISTS audit_log (
			id TEXT PRIMARY KEY,
			actor_type TEXT NOT NULL,
			actor_id TEXT NOT NULL,
			action TEXT NOT NULL,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			summary TEXT NOT NULL,
			detail_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		)`,
	}

// ---- Migrations ----

func migrate(db *sql.DB) error {
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
		if columnExists(db, m.table, m.column) {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			// Ignore errors from duplicate migrations
			if !strings.Contains(err.Error(), "duplicate column") {
				return fmt.Errorf("migration %s.%s: %w", m.table, m.column, err)
			}
		}
	}
	// Backfill ideas
	db.Exec(`UPDATE ideas SET current_summary = CASE WHEN current_summary = '' THEN summary ELSE current_summary END,
		updated_at = CASE WHEN updated_at = '' THEN created_at ELSE updated_at END`)

	// audit_log migration for existing databases.
	if !tableOrVTableExists(db, "audit_log") {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS audit_log (id TEXT PRIMARY KEY, actor_type TEXT NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, summary TEXT NOT NULL, detail_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL)`); err != nil {
			return fmt.Errorf("migration audit_log: %w", err)
		}
	}

	// meeting_rooms new columns (agent_roles_context, auto_arbitrate, meeting_mode).
	if !columnExists(db, "meeting_rooms", "agent_roles_context") {
		db.Exec("ALTER TABLE meeting_rooms ADD COLUMN agent_roles_context TEXT DEFAULT ''")
	}
	if !columnExists(db, "meeting_rooms", "auto_arbitrate") {
		db.Exec("ALTER TABLE meeting_rooms ADD COLUMN auto_arbitrate INTEGER DEFAULT 0")
	}
	if !columnExists(db, "meeting_rooms", "meeting_mode") {
		db.Exec("ALTER TABLE meeting_rooms ADD COLUMN meeting_mode TEXT DEFAULT 'discussion'")
	}
	if !columnExists(db, "meeting_turns", "reply_to") {
		db.Exec("ALTER TABLE meeting_turns ADD COLUMN reply_to TEXT DEFAULT ''")
	}
	if !columnExists(db, "meeting_turns", "address_to") {
		db.Exec("ALTER TABLE meeting_turns ADD COLUMN address_to TEXT DEFAULT ''")
	}

	// embedding column migration.
	if !columnExists(db, "discussion_log", "embedding_json") {
		db.Exec("ALTER TABLE discussion_log ADD COLUMN embedding_json TEXT DEFAULT ''")
	}

	// discussion_log migration.
	if !tableOrVTableExists(db, "discussion_log") {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS discussion_log (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', content TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
			return fmt.Errorf("migration discussion_log: %w", err)
		}
	}

	// source column migration for discussion_log.
	if !columnExists(db, "discussion_log", "source") {
		db.Exec("ALTER TABLE discussion_log ADD COLUMN source TEXT NOT NULL DEFAULT ''")
	}

	// pm_typing migration.
	if !columnExists(db, "meeting_rooms", "pm_typing") {
		db.Exec("ALTER TABLE meeting_rooms ADD COLUMN pm_typing INTEGER DEFAULT 0")
	}

		// source column migration for discussion_log.
	if !columnExists(db, "discussion_log", "source") {
		db.Exec("ALTER TABLE discussion_log ADD COLUMN source TEXT NOT NULL DEFAULT ''")
	}

	// pm_typing migration.
	if !columnExists(db, "meeting_rooms", "pm_typing") {
		db.Exec("ALTER TABLE meeting_rooms ADD COLUMN pm_typing INTEGER DEFAULT 0")
	}

	// meeting / assignment tables migration for existing databases.
	for _, spec := range []struct{ table, sql string }{
		{"meeting_rooms", `CREATE TABLE IF NOT EXISTS meeting_rooms (id TEXT PRIMARY KEY, title TEXT NOT NULL, topic TEXT NOT NULL, context TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'active', agent_roles_context TEXT NOT NULL DEFAULT '', auto_arbitrate INTEGER NOT NULL DEFAULT 0, meeting_mode TEXT NOT NULL DEFAULT 'discussion', created_by TEXT NOT NULL, created_at TEXT NOT NULL, closed_at TEXT)`},
		{"meeting_turns", `CREATE TABLE IF NOT EXISTS meeting_turns (id TEXT PRIMARY KEY, room_id TEXT NOT NULL, turn_number INTEGER NOT NULL, speaker_type TEXT NOT NULL, speaker_id TEXT NOT NULL, question TEXT NOT NULL, response TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'waiting', reply_to TEXT NOT NULL DEFAULT '', address_to TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, FOREIGN KEY(room_id) REFERENCES meeting_rooms(id))`},
		{"meeting_participants", `CREATE TABLE IF NOT EXISTS meeting_participants (meeting_id TEXT NOT NULL, agent_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending', confirmed_at TEXT NOT NULL, PRIMARY KEY (meeting_id, agent_id), FOREIGN KEY(meeting_id) REFERENCES meeting_rooms(id), FOREIGN KEY(agent_id) REFERENCES agent_profiles(id))`},
		{"agent_assignments", `CREATE TABLE IF NOT EXISTS agent_assignments (id TEXT PRIMARY KEY, agent_id TEXT NOT NULL, task_id TEXT, role TEXT NOT NULL, scope TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'assigned', assigned_by TEXT NOT NULL, assigned_at TEXT NOT NULL, claimed_at TEXT, completed_at TEXT, FOREIGN KEY(agent_id) REFERENCES agent_profiles(id), FOREIGN KEY(task_id) REFERENCES tasks(id))`},
	} {
		if !tableOrVTableExists(db, spec.table) {
			if _, err := db.Exec(spec.sql); err != nil {
				return fmt.Errorf("migration %s: %w", spec.table, err)
			}
		}
	}

	// agent_profiles migration — create table if missing in existing databases.
	if !tableOrVTableExists(db, "agent_profiles") {
		if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'coder',
			capabilities TEXT NOT NULL DEFAULT '[]',
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`); err != nil {
			return fmt.Errorf("migration agent_profiles: %w", err)
		}
	}

	// meeting_participants.last_seen_turn migration.
	if !columnExists(db, "meeting_participants", "last_seen_turn") {
		db.Exec("ALTER TABLE meeting_participants ADD COLUMN last_seen_turn INTEGER DEFAULT 0")
	}

	// FTS5 migration for existing databases.
	if !tableOrVTableExists(db, "fts5_index") {
		if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS fts5_index USING fts5(
			content, entity_type UNINDEXED, entity_id UNINDEXED, title,
			tokenize='unicode61'
		)`); err != nil {
			return fmt.Errorf("migration fts5_index: %w", err)
		}
		rebuildFTS5Index(db)
	}

	return nil
}

func tableOrVTableExists(db *sql.DB, name string) bool {
	// PRAGMA table_info doesn't work for FTS5 virtual tables,
	// so use sqlite_master which covers both.
	var count int
	db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = ?", name).Scan(&count)
	return count > 0
}

func columnExists(db *sql.DB, table, column string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk)
		if name == column {
			return true
		}
	}
	return false
}

// ---- Runtime config ----

type RuntimeConfig struct {
	WebHost     string `json:"web_host"`
	WebPort     int    `json:"web_port"`
	AIEndpoint          string `json:"ai_endpoint,omitempty"`
	AIEmbeddingEndpoint string `json:"ai_embedding_endpoint,omitempty"`
	AIModel             string `json:"ai_model,omitempty"`
	AIChatModel         string `json:"ai_chat_model,omitempty"`
}

func loadConfig() RuntimeConfig {
	cfg := RuntimeConfig{WebHost: "127.0.0.1", WebPort: 8720}
	// Env-var overrides take precedence before config.json is read
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
	dir, err := findRuntimeDir()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return cfg
	}
	// Simple JSON parse into config — only fill fields not already set by env
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

func saveConfig(cfg RuntimeConfig) error {
	dir, err := findRuntimeDir()
	if err != nil {
		os.MkdirAll(dir, 0755)
		dir, _ = findRuntimeDir()
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), mustMarshal(cfg), 0644)
}

func findRuntimeDir() (string, error) {
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
			break // reached filesystem root (e.g. D: on Windows)
		}
		dir = parent
	}
	return filepath.Join(cwd, ".pmai"), nil
}

// rebuildFTS5Index repopulates the FTS5 index from all entity tables.
// Safe to call during migration or after schema changes.
func rebuildFTS5Index(db *sql.DB) {
	db.Exec("DELETE FROM fts5_index")

	// Tasks
	rows, err := db.Query("SELECT id, title, last_note FROM tasks")
	if err == nil {
		for rows.Next() {
			var id, title, note string
			rows.Scan(&id, &title, &note)
			db.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'task', ?, ?)",
				title+" "+note, id, title)
		}
		rows.Close()
	}

	// Plans
	rows2, err := db.Query("SELECT id, title, goal FROM plans")
	if err == nil {
		for rows2.Next() {
			var id, title, goal string
			rows2.Scan(&id, &title, &goal)
			db.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'plan', ?, ?)",
				title+" "+goal, id, title)
		}
		rows2.Close()
	}

	// Commits
	rows3, err := db.Query("SELECT id, title, summary FROM commits")
	if err == nil {
		for rows3.Next() {
			var id, title, summary string
			rows3.Scan(&id, &title, &summary)
			db.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'commit', ?, ?)",
				title+" "+summary, id, title)
		}
		rows3.Close()
	}

	// Bugs
	rows4, err := db.Query("SELECT id, title, description, error, root_cause, fix, tags FROM bugs")
	if err == nil {
		for rows4.Next() {
			var id, title, desc, errStr, root, fix, tags string
			rows4.Scan(&id, &title, &desc, &errStr, &root, &fix, &tags)
			db.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'bug', ?, ?)",
				title+" "+desc+" "+errStr+" "+root+" "+fix+" "+tags, id, title)
		}
		rows4.Close()
	}

	// Decisions
	rows5, err := db.Query("SELECT id, title, background, decision_text FROM decisions")
	if err == nil {
		for rows5.Next() {
			var id, title, bg, dt string
			rows5.Scan(&id, &title, &bg, &dt)
			db.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'decision', ?, ?)",
				title+" "+bg+" "+dt, id, title)
		}
		rows5.Close()
	}

	// Ideas
	rows6, err := db.Query("SELECT id, title, summary FROM ideas")
	if err == nil {
		for rows6.Next() {
			var id, title, summary string
			rows6.Scan(&id, &title, &summary)
			db.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'idea', ?, ?)",
				title+" "+summary, id, title)
		}
		rows6.Close()
	}

	// Threads
	rows7, err := db.Query("SELECT id, title, summary FROM threads")
	if err == nil {
		for rows7.Next() {
			var id, title, summary string
			rows7.Scan(&id, &title, &summary)
			db.Exec("INSERT INTO fts5_index (content, entity_type, entity_id, title) VALUES (?, 'thread', ?, ?)",
				title+" "+summary, id, title)
		}
		rows7.Close()
	}
}
