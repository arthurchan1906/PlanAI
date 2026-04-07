from __future__ import annotations

import json
import os
import sqlite3
import subprocess
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional
from uuid import uuid4


RUNTIME_DIRNAME = ".pmai"
DB_FILENAME = "pmai.db"
CONFIG_FILENAME = "config.json"
DEFAULT_WEB_HOST = "127.0.0.1"
DEFAULT_WEB_PORT = 8011
RECOVERY_DIR = Path.home() / ".codex" / "memories"
RECOVERED_DB_PATH = RECOVERY_DIR / "pmai.recovered.db"


def now_iso() -> str:
    return datetime.now().isoformat(timespec="seconds")


def today() -> str:
    return datetime.now().strftime("%Y-%m-%d")


def slug(prefix: str) -> str:
    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    return f"{prefix}-{stamp}-{uuid4().hex[:6]}"


def dumps(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False)


def loads(value: Optional[str], default: Any) -> Any:
    if not value:
        return default
    return json.loads(value)


def find_runtime_dir(start: Optional[Path] = None) -> Optional[Path]:
    override = os.getenv("PMAI_HOME") or os.getenv("PLANAI_HOME") or os.getenv("PROJECT_OS_HOME")
    if override:
        return Path(override).expanduser().resolve()

    current = (start or Path.cwd()).resolve()
    for candidate_root in [current, *current.parents]:
        runtime_dir = candidate_root / RUNTIME_DIRNAME
        if runtime_dir.is_dir():
            return runtime_dir
    return None


def get_project_root(start: Optional[Path] = None) -> Path:
    runtime_dir = find_runtime_dir(start)
    if runtime_dir:
        return runtime_dir.parent
    return (start or Path.cwd()).resolve()


def get_runtime_dir(start: Optional[Path] = None, create: bool = False) -> Path:
    runtime_dir = find_runtime_dir(start)
    if runtime_dir is None:
        runtime_dir = get_project_root(start) / RUNTIME_DIRNAME
    if create:
        runtime_dir.mkdir(parents=True, exist_ok=True)
    return runtime_dir


def get_db_path(start: Optional[Path] = None, create_parent: bool = False) -> Path:
    runtime_dir = get_runtime_dir(start, create=create_parent)
    data_dir = runtime_dir / "data"
    if create_parent:
        data_dir.mkdir(parents=True, exist_ok=True)
    return data_dir / DB_FILENAME


def get_journal_path(start: Optional[Path] = None) -> Path:
    db_path = get_db_path(start)
    return db_path.parent / f"{DB_FILENAME}-journal"


def get_config_path(start: Optional[Path] = None, create_parent: bool = False) -> Path:
    return get_runtime_dir(start, create=create_parent) / CONFIG_FILENAME


def load_runtime_config(start: Optional[Path] = None) -> Dict[str, Any]:
    config = {"web_host": DEFAULT_WEB_HOST, "web_port": DEFAULT_WEB_PORT}
    config_path = get_config_path(start)
    if not config_path.exists():
        return config
    try:
        loaded = json.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return config
    if isinstance(loaded, dict):
        config.update(loaded)
    return config


def save_runtime_config(payload: Dict[str, Any], start: Optional[Path] = None) -> Dict[str, Any]:
    config = load_runtime_config(start)
    config.update(payload)
    config_path = get_config_path(start, create_parent=True)
    config_path.write_text(json.dumps(config, ensure_ascii=False, indent=2), encoding="utf-8")
    return config


def describe_runtime(start: Optional[Path] = None) -> Dict[str, str]:
    return {
        "project_root": str(get_project_root(start)),
        "runtime_dir": str(get_runtime_dir(start)),
        "database": str(get_db_path(start)),
        "config": str(get_config_path(start)),
    }


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
        """
    )


def bootstrap_database(start: Optional[Path] = None) -> Path:
    db_path = get_db_path(start, create_parent=True)
    conn = sqlite3.connect(db_path)
    try:
        ensure_runtime_schema(conn)
        conn.commit()
    finally:
        conn.close()
    return db_path


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
    return conn


def empty_canon() -> Dict[str, Any]:
    return {
        "id": "canon-current",
        "updated_at": None,
        "product_goal": "",
        "engineering_focus": "",
        "architecture": "",
        "version_scope": [],
        "avoid_now": [],
        "top_tasks": [],
        "source_docs": [],
        "related_decisions": [],
    }


def _run_git(args: List[str]) -> Optional[str]:
    try:
        completed = subprocess.run(
            ["git", "-c", "core.quotepath=false", *args],
            cwd=str(get_project_root()),
            capture_output=True,
            text=True,
            check=False,
            encoding="utf-8",
        )
    except OSError:
        return None
    if completed.returncode != 0:
        return None
    return completed.stdout.strip()


def infer_git_metadata() -> Dict[str, Any]:
    branch = _run_git(["rev-parse", "--abbrev-ref", "HEAD"]) or ""
    commit_hash = _run_git(["rev-parse", "HEAD"]) or ""

    files: List[str] = []
    for args in [
        ["diff", "--name-only", "--relative"],
        ["diff", "--name-only", "--relative", "--cached"],
        ["ls-files", "--others", "--exclude-standard"],
    ]:
        output = _run_git(args)
        if not output:
            continue
        for line in output.splitlines():
            candidate = line.strip().replace("\\", "/")
            if candidate.startswith('"') and candidate.endswith('"'):
                candidate = candidate[1:-1]
            if candidate.startswith(".pmai/"):
                continue
            if candidate and candidate not in files:
                files.append(candidate)

    return {
        "branch": branch,
        "commit_hash": commit_hash,
        "files": files,
    }


def fetch_canon() -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM canon WHERE id = 1").fetchone()
        if not row:
            return empty_canon()
        items = conn.execute(
            "SELECT item_type, position, value FROM canon_items ORDER BY item_type, position"
        ).fetchall()
        grouped: Dict[str, List[str]] = {}
        for item in items:
            grouped.setdefault(item["item_type"], []).append(item["value"])
        return {
            "id": "canon-current",
            "updated_at": row["updated_at"],
            "product_goal": row["product_goal"],
            "engineering_focus": row["engineering_focus"],
            "architecture": row["architecture"],
            "version_scope": grouped.get("version_scope", []),
            "avoid_now": grouped.get("avoid_now", []),
            "top_tasks": grouped.get("top_tasks", []),
            "source_docs": grouped.get("source_docs", []),
            "related_decisions": grouped.get("related_decisions", []),
        }
    finally:
        conn.close()


def update_canon(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        decision = conn.execute(
            "SELECT * FROM decisions WHERE id = ? AND status = 'accepted'",
            (payload["decision_id"],),
        ).fetchone()
        if not decision:
            raise KeyError(payload["decision_id"])

        row = conn.execute("SELECT * FROM canon WHERE id = 1").fetchone()
        if not row:
            conn.execute(
                """
                INSERT INTO canon (id, updated_at, product_goal, engineering_focus, architecture)
                VALUES (1, ?, ?, ?, ?)
                """,
                (today(), "", "", ""),
            )
            conn.commit()

        canon = fetch_canon()
        version_scope = canon["version_scope"][:]
        avoid_now = canon["avoid_now"][:]
        related_decisions = canon["related_decisions"][:]

        for item in payload.get("add_scope", []):
            if item and item not in version_scope:
                version_scope.append(item)
        for item in payload.get("add_avoid", []):
            if item and item not in avoid_now:
                avoid_now.append(item)
        if payload["decision_id"] not in related_decisions:
            related_decisions.append(payload["decision_id"])

        conn.execute(
            """
            UPDATE canon
            SET updated_at = ?, product_goal = ?, engineering_focus = ?, architecture = ?
            WHERE id = 1
            """,
            (
                today(),
                payload.get("product_goal") or canon["product_goal"],
                payload.get("engineering_focus") or canon["engineering_focus"],
                payload.get("architecture") or canon["architecture"],
            ),
        )
        conn.execute("DELETE FROM canon_items WHERE item_type = 'version_scope'")
        conn.execute("DELETE FROM canon_items WHERE item_type = 'avoid_now'")
        conn.execute("DELETE FROM canon_items WHERE item_type = 'related_decisions'")
        for idx, value in enumerate(version_scope):
            conn.execute(
                "INSERT INTO canon_items (item_type, position, value) VALUES (?, ?, ?)",
                ("version_scope", idx, value),
            )
        for idx, value in enumerate(avoid_now):
            conn.execute(
                "INSERT INTO canon_items (item_type, position, value) VALUES (?, ?, ?)",
                ("avoid_now", idx, value),
            )
        for idx, value in enumerate(related_decisions):
            conn.execute(
                "INSERT INTO canon_items (item_type, position, value) VALUES (?, ?, ?)",
                ("related_decisions", idx, value),
            )
        conn.commit()
        return fetch_canon()
    finally:
        conn.close()


def list_tasks(status: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = """
            SELECT * FROM tasks
            """
        params: List[Any] = []
        if status:
            query += " WHERE status = ?"
            params.append(status)
        query += """
            ORDER BY CASE status
                WHEN 'in_progress' THEN 0
                WHEN 'todo' THEN 1
                WHEN 'blocked' THEN 2
                ELSE 3
            END, priority, updated_at DESC
            """
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "status": row["status"],
                "priority": row["priority"],
                "phase": row["phase"],
                "acceptance": loads(row["acceptance_json"], []),
                "related_docs": loads(row["related_docs_json"], []),
                "related_decisions": loads(row["related_decisions_json"], []),
                "last_note": row["last_note"] or "",
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def create_task(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        task_id = slug("task")
        conn.execute(
            """
            INSERT INTO tasks (
                id, title, status, priority, phase,
                acceptance_json, related_docs_json, related_decisions_json, last_note, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                task_id,
                payload["title"],
                payload.get("status", "todo"),
                payload.get("priority", "P1"),
                payload.get("phase", "general"),
                dumps(payload.get("acceptance", [])),
                dumps([]),
                dumps([]),
                "",
                today(),
            ),
        )
        conn.commit()
        created = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        return {
            "id": created["id"],
            "title": created["title"],
            "status": created["status"],
            "priority": created["priority"],
            "phase": created["phase"],
            "acceptance": loads(created["acceptance_json"], []),
            "related_docs": loads(created["related_docs_json"], []),
            "related_decisions": loads(created["related_decisions_json"], []),
            "last_note": created["last_note"] or "",
            "updated_at": created["updated_at"],
        }
    finally:
        conn.close()


def update_task(
    task_id: str,
    status: str,
    note: str = "",
    allow_without_commit: bool = False,
) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if not row:
            raise KeyError(task_id)
        if status == "done" and row["status"] != "done" and not allow_without_commit:
            ready_commit = conn.execute(
                """
                SELECT id
                FROM commits
                WHERE task_id = ?
                  AND status IN ('committed', 'merged')
                  AND review_status = 'approved'
                ORDER BY updated_at DESC, created_at DESC, id DESC
                LIMIT 1
                """,
                (task_id,),
            ).fetchone()
            if not ready_commit:
                raise ValueError(
                    "task cannot be marked done without at least one approved commit "
                    "(status=committed|merged, review_status=approved) linked by --task-id"
                )
        conn.execute(
            "UPDATE tasks SET status = ?, last_note = ?, updated_at = ? WHERE id = ?",
            (status, note, today(), task_id),
        )
        conn.commit()
        updated = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        return {
            "id": updated["id"],
            "title": updated["title"],
            "status": updated["status"],
            "priority": updated["priority"],
            "phase": updated["phase"],
            "acceptance": loads(updated["acceptance_json"], []),
            "related_docs": loads(updated["related_docs_json"], []),
            "related_decisions": loads(updated["related_decisions_json"], []),
            "last_note": updated["last_note"] or "",
            "updated_at": updated["updated_at"],
        }
    finally:
        conn.close()


def list_commits(
    status: Optional[str] = None,
    task_id: Optional[str] = None,
    decision_id: Optional[str] = None,
) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM commits"
        params: List[Any] = []
        where_clauses: List[str] = []
        if status:
            where_clauses.append("status = ?")
            params.append(status)
        if task_id:
            where_clauses.append("task_id = ?")
            params.append(task_id)
        if decision_id:
            where_clauses.append("decision_id = ?")
            params.append(decision_id)
        if where_clauses:
            query += " WHERE " + " AND ".join(where_clauses)
        query += " ORDER BY created_at DESC, id DESC"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "branch": row["branch"],
                "commit_hash": row["commit_hash"],
                "task_id": row["task_id"],
                "decision_id": row["decision_id"],
                "status": row["status"],
                "test_status": row["test_status"],
                "review_status": row["review_status"],
                "files": loads(row["files_json"], []),
                "created_at": row["created_at"],
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def create_commit(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        if payload.get("auto_git"):
            git_meta = infer_git_metadata()
        else:
            git_meta = {}

        if payload.get("task_id"):
            task = conn.execute("SELECT id FROM tasks WHERE id = ?", (payload["task_id"],)).fetchone()
            if not task:
                raise KeyError(payload["task_id"])
        if payload.get("decision_id"):
            decision = conn.execute("SELECT id FROM decisions WHERE id = ?", (payload["decision_id"],)).fetchone()
            if not decision:
                raise KeyError(payload["decision_id"])

        commit_id = slug("commit")
        created_at = now_iso()
        row = {
            "id": commit_id,
            "title": payload["title"],
            "summary": payload.get("summary", ""),
            "branch": payload.get("branch") or git_meta.get("branch", ""),
            "commit_hash": payload.get("commit_hash") or git_meta.get("commit_hash", ""),
            "task_id": payload.get("task_id"),
            "decision_id": payload.get("decision_id"),
            "status": payload.get("status", "draft"),
            "test_status": payload.get("test_status", "not_run"),
            "review_status": payload.get("review_status", "pending"),
            "files": payload.get("files", []) or git_meta.get("files", []),
            "created_at": created_at,
            "updated_at": created_at,
        }
        conn.execute(
            """
            INSERT INTO commits (
                id, title, summary, branch, commit_hash, task_id, decision_id,
                status, test_status, review_status, files_json, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["summary"],
                row["branch"],
                row["commit_hash"],
                row["task_id"],
                row["decision_id"],
                row["status"],
                row["test_status"],
                row["review_status"],
                dumps(row["files"]),
                row["created_at"],
                row["updated_at"],
            ),
        )
        conn.commit()
        return row
    finally:
        conn.close()


def update_commit(commit_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM commits WHERE id = ?", (commit_id,)).fetchone()
        if not row:
            raise KeyError(commit_id)

        next_task_id = row["task_id"]
        if payload.get("clear_task_id"):
            next_task_id = None
        elif "task_id" in payload and payload.get("task_id"):
            task = conn.execute("SELECT id FROM tasks WHERE id = ?", (payload["task_id"],)).fetchone()
            if not task:
                raise KeyError(payload["task_id"])
            next_task_id = payload["task_id"]

        next_decision_id = row["decision_id"]
        if payload.get("clear_decision_id"):
            next_decision_id = None
        elif "decision_id" in payload and payload.get("decision_id"):
            decision = conn.execute("SELECT id FROM decisions WHERE id = ?", (payload["decision_id"],)).fetchone()
            if not decision:
                raise KeyError(payload["decision_id"])
            next_decision_id = payload["decision_id"]

        next_files = loads(row["files_json"], [])
        if payload.get("files") is not None:
            next_files = payload.get("files") or []

        conn.execute(
            """
            UPDATE commits
            SET title = ?, summary = ?, branch = ?, commit_hash = ?, task_id = ?, decision_id = ?,
                status = ?, test_status = ?, review_status = ?, files_json = ?, updated_at = ?
            WHERE id = ?
            """,
            (
                payload.get("title") if payload.get("title") is not None else row["title"],
                payload.get("summary") if payload.get("summary") is not None else row["summary"],
                payload.get("branch") if payload.get("branch") is not None else row["branch"],
                payload.get("commit_hash") if payload.get("commit_hash") is not None else row["commit_hash"],
                next_task_id,
                next_decision_id,
                payload.get("status") if payload.get("status") is not None else row["status"],
                payload.get("test_status") if payload.get("test_status") is not None else row["test_status"],
                payload.get("review_status") if payload.get("review_status") is not None else row["review_status"],
                dumps(next_files),
                now_iso(),
                commit_id,
            ),
        )
        conn.commit()
        updated = conn.execute("SELECT * FROM commits WHERE id = ?", (commit_id,)).fetchone()
        return {
            "id": updated["id"],
            "title": updated["title"],
            "summary": updated["summary"],
            "branch": updated["branch"],
            "commit_hash": updated["commit_hash"],
            "task_id": updated["task_id"],
            "decision_id": updated["decision_id"],
            "status": updated["status"],
            "test_status": updated["test_status"],
            "review_status": updated["review_status"],
            "files": loads(updated["files_json"], []),
            "created_at": updated["created_at"],
            "updated_at": updated["updated_at"],
        }
    finally:
        conn.close()


def list_decisions() -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        rows = conn.execute("SELECT * FROM decisions ORDER BY date DESC, id DESC").fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "date": row["date"],
                "status": row["status"],
                "background": row["background"],
                "decision": row["decision_text"],
                "impact": loads(row["impact_json"], []),
                "alternatives": loads(row["alternatives_json"], []),
                "related_tasks": loads(row["related_tasks_json"], []),
                "updates_canon": bool(row["updates_canon"]),
            }
            for row in rows
        ]
    finally:
        conn.close()


def create_decision(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        decision_id = slug("decision")
        row = {
            "id": decision_id,
            "title": payload["title"],
            "date": today(),
            "status": payload.get("status", "proposed"),
            "background": payload["background"],
            "decision": payload["decision"],
            "impact": payload.get("impact", []),
            "alternatives": payload.get("alternatives", []),
            "related_tasks": payload.get("related_tasks", []),
            "updates_canon": bool(payload.get("updates_canon", False)),
        }
        conn.execute(
            """
            INSERT INTO decisions (
                id, title, date, status, background, decision_text,
                impact_json, alternatives_json, related_tasks_json, updates_canon
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["date"],
                row["status"],
                row["background"],
                row["decision"],
                dumps(row["impact"]),
                dumps(row["alternatives"]),
                dumps(row["related_tasks"]),
                1 if row["updates_canon"] else 0,
            ),
        )
        conn.commit()
        return row
    finally:
        conn.close()


def update_decision_status(decision_id: str, status: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM decisions WHERE id = ?", (decision_id,)).fetchone()
        if not row:
            raise KeyError(decision_id)
        conn.execute("UPDATE decisions SET status = ? WHERE id = ?", (status, decision_id))
        conn.commit()
        updated = conn.execute("SELECT * FROM decisions WHERE id = ?", (decision_id,)).fetchone()
        return {
            "id": updated["id"],
            "title": updated["title"],
            "date": updated["date"],
            "status": updated["status"],
            "background": updated["background"],
            "decision": updated["decision_text"],
            "impact": loads(updated["impact_json"], []),
            "alternatives": loads(updated["alternatives_json"], []),
            "related_tasks": loads(updated["related_tasks_json"], []),
            "updates_canon": bool(updated["updates_canon"]),
        }
    finally:
        conn.close()


def list_ideas(status: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM ideas"
        params: List[Any] = []
        if status:
            query += " WHERE status = ?"
            params.append(status)
        query += " ORDER BY created_at DESC"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "impact": row["impact"],
                "source": row["source"],
                "status": row["status"],
                "canon_conflict": bool(row["canon_conflict"]),
                "created_at": row["created_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def create_idea(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        idea_id = slug("idea")
        row = {
            "id": idea_id,
            "title": payload["title"],
            "summary": payload["summary"],
            "impact": payload.get("impact", ""),
            "source": payload.get("source", "web"),
            "status": "inbox",
            "canon_conflict": 1 if payload.get("canon_conflict") else 0,
            "created_at": now_iso(),
        }
        conn.execute(
            """
            INSERT INTO ideas (
                id, title, summary, impact, source, status, canon_conflict, created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["summary"],
                row["impact"],
                row["source"],
                row["status"],
                row["canon_conflict"],
                row["created_at"],
            ),
        )
        conn.commit()
        row["canon_conflict"] = bool(row["canon_conflict"])
        return row
    finally:
        conn.close()


def review_idea(idea_id: str, status: str, note: str = "") -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        if not row:
            raise KeyError(idea_id)
        summary = row["summary"]
        if note:
            summary = summary.rstrip() + f"\n\n[review-note] {note}"
        conn.execute(
            "UPDATE ideas SET status = ?, summary = ? WHERE id = ?",
            (status, summary, idea_id),
        )
        conn.commit()
        updated = conn.execute("SELECT * FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        return {
            "id": updated["id"],
            "title": updated["title"],
            "summary": updated["summary"],
            "impact": updated["impact"],
            "source": updated["source"],
            "status": updated["status"],
            "canon_conflict": bool(updated["canon_conflict"]),
            "created_at": updated["created_at"],
        }
    finally:
        conn.close()


def list_doc_records(status: Optional[str] = None, layer: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM doc_records"
        params: List[Any] = []
        conditions: List[str] = []
        if status:
            conditions.append("status = ?")
            params.append(status)
        if layer:
            conditions.append("layer = ?")
            params.append(layer)
        if conditions:
            query += " WHERE " + " AND ".join(conditions)
        query += " ORDER BY source_of_truth DESC, status, path"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "path": row["path"],
                "type": row["type"],
                "status": row["status"],
                "layer": row["layer"],
                "source_of_truth": bool(row["source_of_truth"]),
                "last_reviewed": row["last_reviewed"],
                "superseded_by": row["superseded_by"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def update_doc_record(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        path = payload["path"]
        row = conn.execute("SELECT * FROM doc_records WHERE path = ?", (path,)).fetchone()
        if not row and not payload.get("create"):
            raise KeyError(path)
        if not row:
            conn.execute(
                """
                INSERT INTO doc_records (
                    path, type, status, layer, source_of_truth, last_reviewed, superseded_by
                ) VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    path,
                    payload.get("type", "unknown"),
                    payload.get("status", "draft"),
                    payload.get("layer", "exploration"),
                    1 if payload.get("source_of_truth") else 0,
                    payload.get("last_reviewed") or today(),
                    payload.get("superseded_by"),
                ),
            )
        else:
            next_type = payload.get("type") or row["type"]
            next_status = payload.get("status") or row["status"]
            next_layer = payload.get("layer") or row["layer"]
            next_source = row["source_of_truth"]
            if payload.get("source_of_truth") is True:
                next_source = 1
            if payload.get("clear_source_of_truth"):
                next_source = 0
            next_reviewed = payload.get("last_reviewed") or today()
            next_superseded_by = row["superseded_by"]
            if "superseded_by" in payload:
                next_superseded_by = payload.get("superseded_by")
            conn.execute(
                """
                UPDATE doc_records
                SET type = ?, status = ?, layer = ?, source_of_truth = ?, last_reviewed = ?, superseded_by = ?
                WHERE path = ?
                """,
                (
                    next_type,
                    next_status,
                    next_layer,
                    next_source,
                    next_reviewed,
                    next_superseded_by,
                    path,
                ),
            )
        conn.commit()
        updated = conn.execute("SELECT * FROM doc_records WHERE path = ?", (path,)).fetchone()
        return {
            "path": updated["path"],
            "type": updated["type"],
            "status": updated["status"],
            "layer": updated["layer"],
            "source_of_truth": bool(updated["source_of_truth"]),
            "last_reviewed": updated["last_reviewed"],
            "superseded_by": updated["superseded_by"],
        }
    finally:
        conn.close()


def audit_docs() -> Dict[str, Any]:
    rows = list_doc_records()
    active_records = [row for row in rows if row["status"] == "active"]
    source_of_truth_records = [row for row in rows if row["source_of_truth"]]
    obsolete_without_replacement = [
        row["path"] for row in rows if row["status"] == "obsolete" and not row["superseded_by"]
    ]
    invalid_truth_records = [
        row["path"]
        for row in rows
        if row["status"] in {"archived", "obsolete"} and row["source_of_truth"]
    ]
    return {
        "database": str(get_db_path()),
        "total_records": len(rows),
        "active_records": len(active_records),
        "source_of_truth_records": len(source_of_truth_records),
        "obsolete_without_replacement": obsolete_without_replacement,
        "invalid_truth_records": invalid_truth_records,
    }


def get_daily_note(note_date: Optional[str] = None) -> Dict[str, Any]:
    target_date = note_date or today()
    conn = get_connection()
    try:
        row = conn.execute(
            "SELECT * FROM daily_notes WHERE note_date = ?",
            (target_date,),
        ).fetchone()
        if not row:
            return {
                "note_date": target_date,
                "completed": [],
                "problems": [],
                "risks": [],
                "next": [],
                "updated_at": None,
            }
        return {
            "note_date": row["note_date"],
            "completed": loads(row["completed_json"], []),
            "problems": loads(row["problems_json"], []),
            "risks": loads(row["risks_json"], []),
            "next": loads(row["next_json"], []),
            "updated_at": row["updated_at"],
        }
    finally:
        conn.close()


def list_daily_notes(limit: int = 30) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        rows = conn.execute(
            "SELECT note_date, updated_at FROM daily_notes ORDER BY note_date DESC LIMIT ?",
            (limit,),
        ).fetchall()
        return [
            {"note_date": row["note_date"], "updated_at": row["updated_at"]}
            for row in rows
        ]
    finally:
        conn.close()


def append_daily_note(payload: Dict[str, Any], note_date: Optional[str] = None) -> Dict[str, Any]:
    target_date = note_date or today()
    current = get_daily_note(target_date)
    completed = current["completed"] + payload.get("completed", [])
    problems = current["problems"] + payload.get("problems", [])
    risks = current["risks"] + payload.get("risks", [])
    next_items = current["next"] + payload.get("next", [])
    conn = get_connection()
    try:
        conn.execute(
            """
            INSERT INTO daily_notes (
                note_date, completed_json, problems_json, risks_json, next_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(note_date) DO UPDATE SET
                completed_json = excluded.completed_json,
                problems_json = excluded.problems_json,
                risks_json = excluded.risks_json,
                next_json = excluded.next_json,
                updated_at = excluded.updated_at
            """,
            (
                target_date,
                dumps(completed),
                dumps(problems),
                dumps(risks),
                dumps(next_items),
                now_iso(),
            ),
        )
        conn.commit()
        return get_daily_note(target_date)
    finally:
        conn.close()


def replace_daily_note(payload: Dict[str, Any], note_date: Optional[str] = None) -> Dict[str, Any]:
    target_date = note_date or today()
    conn = get_connection()
    try:
        conn.execute(
            """
            INSERT INTO daily_notes (
                note_date, completed_json, problems_json, risks_json, next_json, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?)
            ON CONFLICT(note_date) DO UPDATE SET
                completed_json = excluded.completed_json,
                problems_json = excluded.problems_json,
                risks_json = excluded.risks_json,
                next_json = excluded.next_json,
                updated_at = excluded.updated_at
            """,
            (
                target_date,
                dumps(payload.get("completed", [])),
                dumps(payload.get("problems", [])),
                dumps(payload.get("risks", [])),
                dumps(payload.get("next", [])),
                now_iso(),
            ),
        )
        conn.commit()
        return get_daily_note(target_date)
    finally:
        conn.close()


def get_dashboard_summary() -> Dict[str, Any]:
    canon = fetch_canon()
    tasks = list_tasks()
    ideas = list_ideas()
    decisions = list_decisions()
    docs = list_doc_records()
    commits = list_commits()
    daily = get_daily_note()
    current_recommendations = daily["next"][:4]
    current_risks = daily["risks"][:3]
    return {
        "canon_updated_at": canon["updated_at"],
        "task_counts": {
            "total": len(tasks),
            "in_progress": len([task for task in tasks if task["status"] == "in_progress"]),
            "todo": len([task for task in tasks if task["status"] == "todo"]),
            "blocked": len([task for task in tasks if task["status"] == "blocked"]),
        },
        "idea_counts": {
            "total": len(ideas),
            "inbox": len([idea for idea in ideas if idea["status"] == "inbox"]),
            "accepted": len([idea for idea in ideas if idea["status"] == "accepted"]),
        },
        "decision_counts": {
            "total": len(decisions),
            "accepted": len([decision for decision in decisions if decision["status"] == "accepted"]),
            "proposed": len([decision for decision in decisions if decision["status"] == "proposed"]),
        },
        "doc_counts": {
            "total": len(docs),
            "active": len([doc for doc in docs if doc["status"] == "active"]),
            "source_of_truth": len([doc for doc in docs if doc["source_of_truth"]]),
        },
        "commit_counts": {
            "total": len(commits),
            "draft": len([commit for commit in commits if commit["status"] == "draft"]),
            "committed": len([commit for commit in commits if commit["status"] == "committed"]),
            "merged": len([commit for commit in commits if commit["status"] == "merged"]),
            "needs_review": len([commit for commit in commits if commit["review_status"] != "approved"]),
        },
        "today_focus": [
            *[task["title"] for task in tasks if task["status"] == "in_progress"][:3],
            *daily["next"][:3],
        ][:5],
        "current_recommendations": current_recommendations,
        "current_risks": current_risks,
        "recent_commits": commits[:5],
    }
