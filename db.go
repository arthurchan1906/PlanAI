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
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("PMAI database not found: %s — run aipmc init first", dbPath)
	}
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=MEMORY&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, err
	}
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
	return nil
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
	WebHost string `json:"web_host"`
	WebPort int    `json:"web_port"`
}

func loadConfig() RuntimeConfig {
	cfg := RuntimeConfig{WebHost: "127.0.0.1", WebPort: 8720}
	dir, err := findRuntimeDir()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return cfg
	}
	// Simple JSON parse into config
	var raw map[string]any
	if json.Unmarshal(data, &raw) == nil {
		if h, ok := raw["web_host"].(string); ok {
			cfg.WebHost = h
		}
		if p, ok := raw["web_port"].(float64); ok {
			cfg.WebPort = int(p)
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
