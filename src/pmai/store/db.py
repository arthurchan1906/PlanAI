from __future__ import annotations

import sqlite3
from pathlib import Path
from typing import Optional

from .config import get_db_path, get_journal_path, get_project_root

RECOVERY_DIR = Path.home() / ".codex" / "memories"
RECOVERED_DB_PATH = RECOVERY_DIR / "pmai.recovered.db"


def ensure_runtime_schema(conn: sqlite3.Connection) -> None:
    conn.executescript(
        """
        CREATE TABLE IF NOT EXISTS canon (
            id INTEGER PRIMARY KEY CHECK (id = 1),
            updated_at TEXT NOT NULL,
            product_goal TEXT NOT NULL,
            engineering_focus TEXT NOT NULL,
            architecture TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS canon_items (
            item_type TEXT NOT NULL,
            position INTEGER NOT NULL,
            value TEXT NOT NULL,
            PRIMARY KEY (item_type, position)
        );

        CREATE TABLE IF NOT EXISTS visions (
            id TEXT PRIMARY KEY,
            title TEXT NOT NULL,
            summary TEXT NOT NULL,
            status TEXT NOT NULL,
            horizon TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS roadmap (
            id TEXT PRIMARY KEY,
            vision_id TEXT,
            title TEXT NOT NULL,
            target_date TEXT NOT NULL,
            status TEXT NOT NULL,
            priority TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL,
            FOREIGN KEY(vision_id) REFERENCES visions(id)
        );

        CREATE TABLE IF NOT EXISTS plans (
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
        );

        CREATE TABLE IF NOT EXISTS tasks (
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
        );

        CREATE TABLE IF NOT EXISTS principles (
            id TEXT PRIMARY KEY,
            title TEXT NOT NULL,
            summary TEXT NOT NULL,
            kind TEXT NOT NULL,
            status TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS decisions (
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
        );

        CREATE TABLE IF NOT EXISTS ideas (
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
        );

        CREATE TABLE IF NOT EXISTS idea_comments (
            id TEXT PRIMARY KEY,
            idea_id TEXT NOT NULL,
            author_type TEXT NOT NULL,
            author_name TEXT NOT NULL,
            kind TEXT NOT NULL,
            content TEXT NOT NULL,
            created_at TEXT NOT NULL,
            FOREIGN KEY(idea_id) REFERENCES ideas(id)
        );

        CREATE TABLE IF NOT EXISTS links (
            id TEXT PRIMARY KEY,
            source_type TEXT NOT NULL,
            source_id TEXT NOT NULL,
            relation TEXT NOT NULL,
            target_type TEXT NOT NULL,
            target_id TEXT NOT NULL,
            note TEXT NOT NULL,
            created_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS doc_records (
            path TEXT PRIMARY KEY,
            type TEXT NOT NULL,
            status TEXT NOT NULL,
            layer TEXT NOT NULL,
            source_of_truth INTEGER NOT NULL DEFAULT 0,
            last_reviewed TEXT NOT NULL,
            superseded_by TEXT
        );

        CREATE TABLE IF NOT EXISTS daily_notes (
            note_date TEXT PRIMARY KEY,
            completed_json TEXT NOT NULL,
            problems_json TEXT NOT NULL,
            risks_json TEXT NOT NULL,
            next_json TEXT NOT NULL,
            updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS commits (
            id TEXT PRIMARY KEY,
            title TEXT NOT NULL,
            summary TEXT NOT NULL,
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
        );

        CREATE TABLE IF NOT EXISTS task_notes (
            id TEXT PRIMARY KEY,
            task_id TEXT NOT NULL,
            content TEXT NOT NULL,
            mode TEXT NOT NULL,
            created_at TEXT NOT NULL,
            FOREIGN KEY(task_id) REFERENCES tasks(id)
        );
        """
    )


def _recover_database_from_primary(db_path: Path) -> Path:
    RECOVERY_DIR.mkdir(parents=True, exist_ok=True)
    source_uri = f"file:{db_path.as_posix()}?immutable=1"
    source = sqlite3.connect(source_uri, uri=True)
    try:
        target = sqlite3.connect(RECOVERED_DB_PATH)
        try:
            source.backup(target)
        finally:
            target.close()
    finally:
        source.close()
    return RECOVERED_DB_PATH


def get_connection() -> sqlite3.Connection:
    db_path = get_db_path()
    journal_path = get_journal_path()
    if not db_path.exists():
        raise FileNotFoundError(f"PMAI database not found: {db_path}")
    if journal_path.exists():
        target_path = RECOVERED_DB_PATH if RECOVERED_DB_PATH.exists() else _recover_database_from_primary(db_path)
        conn = sqlite3.connect(target_path)
    else:
        target_path = db_path
        try:
            conn = sqlite3.connect(target_path)
            conn.execute("SELECT name FROM sqlite_master LIMIT 1").fetchone()
        except sqlite3.OperationalError as exc:
            if "disk I/O error" not in str(exc):
                raise
            target_path = RECOVERED_DB_PATH if RECOVERED_DB_PATH.exists() else _recover_database_from_primary(db_path)
            conn = sqlite3.connect(target_path)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=MEMORY")
    conn.execute("PRAGMA synchronous=NORMAL")
    ensure_runtime_schema(conn)
    _migrate_database(conn)
    return conn


def _migrate_database(conn: sqlite3.Connection) -> None:
    """数据库迁移：为旧表添加缺失的列"""
    migrations = [
        ("tasks", "roadmap_id", "ALTER TABLE tasks ADD COLUMN roadmap_id TEXT"),
        ("tasks", "plan_id", "ALTER TABLE tasks ADD COLUMN plan_id TEXT"),
        ("tasks", "created_at", "ALTER TABLE tasks ADD COLUMN created_at TEXT NOT NULL DEFAULT ''"),
        ("ideas", "current_summary", "ALTER TABLE ideas ADD COLUMN current_summary TEXT NOT NULL DEFAULT ''"),
        ("ideas", "main_question", "ALTER TABLE ideas ADD COLUMN main_question TEXT NOT NULL DEFAULT ''"),
        ("ideas", "recommended_next_action", "ALTER TABLE ideas ADD COLUMN recommended_next_action TEXT NOT NULL DEFAULT ''"),
        ("ideas", "updated_at", "ALTER TABLE ideas ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''"),
        ("commits", "evidence_summary", "ALTER TABLE commits ADD COLUMN evidence_summary TEXT NOT NULL DEFAULT ''"),
        ("commits", "review_notes", "ALTER TABLE commits ADD COLUMN review_notes TEXT NOT NULL DEFAULT ''"),
    ]
    
    for table_name, column_name, alter_sql in migrations:
        try:
            columns = [row[1] for row in conn.execute(f"PRAGMA table_info({table_name})").fetchall()]
            if column_name not in columns:
                conn.execute(alter_sql)
                conn.commit()
        except Exception:
            pass  # 忽略迁移错误

    try:
        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS idea_comments (
                id TEXT PRIMARY KEY,
                idea_id TEXT NOT NULL,
                author_type TEXT NOT NULL,
                author_name TEXT NOT NULL,
                kind TEXT NOT NULL,
                content TEXT NOT NULL,
                created_at TEXT NOT NULL,
                FOREIGN KEY(idea_id) REFERENCES ideas(id)
            )
            """
        )
        conn.execute(
            """
            UPDATE ideas
            SET current_summary = CASE
                    WHEN current_summary = '' THEN summary
                    ELSE current_summary
                END,
                updated_at = CASE
                    WHEN updated_at = '' THEN created_at
                    ELSE updated_at
                END
            """
        )
        conn.commit()
    except Exception:
        pass

    try:
        conn.execute(
            """
            CREATE TABLE IF NOT EXISTS task_notes (
                id TEXT PRIMARY KEY,
                task_id TEXT NOT NULL,
                content TEXT NOT NULL,
                mode TEXT NOT NULL,
                created_at TEXT NOT NULL,
                FOREIGN KEY(task_id) REFERENCES tasks(id)
            )
            """
        )
        conn.commit()
    except Exception:
        pass


def bootstrap_db(start: Optional[Path] = None) -> Path:
    db_path = get_db_path(start, create_parent=True)
    conn = sqlite3.connect(db_path)
    try:
        ensure_runtime_schema(conn)
        _migrate_database(conn)
        conn.commit()
    finally:
        conn.close()
    return db_path


def bootstrap_database(start: Optional[Path] = None) -> Path:
    return bootstrap_db(start)
