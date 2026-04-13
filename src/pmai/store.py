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

        CREATE TABLE IF NOT EXISTS visions (
            id TEXT PRIMARY KEY,
            title TEXT NOT NULL,
            summary TEXT NOT NULL,
            status TEXT NOT NULL,
            horizon TEXT NOT NULL,
            created_at TEXT NOT NULL,
            updated_at TEXT NOT NULL
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


def _normalize_git_path(value: str) -> str:
    candidate = value.strip().replace('\\', '/')
    if candidate.startswith('"') and candidate.endswith('"'):
        candidate = candidate[1:-1]
    if ' -> ' in candidate:
        candidate = candidate.split(' -> ', 1)[1].strip()
    return candidate



def _normalize_file_list(values: List[str]) -> List[str]:
    files: List[str] = []
    for value in values:
        candidate = _normalize_git_path(value)
        if not candidate:
            continue
        if candidate not in files:
            files.append(candidate)
    return files

def get_git_worktree_status() -> Dict[str, Any]:
    branch = _run_git(["rev-parse", "--abbrev-ref", "HEAD"]) or ""
    output = _run_git(["status", "--short"]) or ""
    files: List[Dict[str, Any]] = []
    staged: List[str] = []
    unstaged: List[str] = []
    untracked: List[str] = []

    for line in output.splitlines():
        if len(line) < 3:
            continue
        index_status = line[0]
        worktree_status = line[1]
        path = _normalize_git_path(line[3:])
        if not path or path.startswith('.pmai/'):
            continue
        item = {
            "path": path,
            "index_status": index_status,
            "worktree_status": worktree_status,
        }
        files.append(item)
        if index_status not in (' ', '?') and path not in staged:
            staged.append(path)
        if worktree_status not in (' ', '?') and path not in unstaged:
            unstaged.append(path)
        if index_status == '?' and worktree_status == '?' and path not in untracked:
            untracked.append(path)

    return {
        "branch": branch,
        "dirty": bool(files),
        "changed_files_count": len(files),
        "staged": staged,
        "unstaged": unstaged,
        "untracked": untracked,
        "files": files,
    }


def get_git_diff_summary(*, staged: bool = False) -> Dict[str, Any]:
    branch = _run_git(["rev-parse", "--abbrev-ref", "HEAD"]) or ""
    worktree = get_git_worktree_status()
    diff_args = ["diff", "--stat", "--compact-summary"]
    name_only_args = ["diff", "--name-only", "--relative"]
    if staged:
        diff_args.append("--cached")
        name_only_args.append("--cached")
    diff_output = _run_git(diff_args) or ""
    names_output = _run_git(name_only_args) or ""
    files = _normalize_file_list(names_output.splitlines())
    stat_lines = [line.rstrip() for line in diff_output.splitlines() if line.strip()]
    return {
        "branch": branch,
        "scope": "staged" if staged else "worktree",
        "dirty": worktree.get("dirty", False),
        "files": files,
        "stats": stat_lines,
        "has_diff": bool(files or stat_lines),
    }


def get_git_diff(*, staged: bool = False) -> Dict[str, Any]:
    """获取 git diff 信息"""
    return get_git_diff_summary(staged=staged)


def get_git_recent_commits(limit: int = 10) -> List[Dict[str, Any]]:
    """获取最近的 git commits"""
    return list_recent_git_commits(limit)


def list_recent_git_commits(limit: int = 5) -> List[Dict[str, Any]]:
    if limit <= 0:
        return []
    output = _run_git([
        "log",
        f"-n{limit}",
        "--date=iso-strict",
        "--name-only",
        "--pretty=format:%H%x1f%an%x1f%ad%x1f%s",
    ])
    if not output:
        return []

    commits: List[Dict[str, Any]] = []
    current: Optional[Dict[str, Any]] = None
    for raw_line in output.splitlines():
        line = raw_line.rstrip()
        if not line:
            continue
        if "" in line:
            if current is not None:
                commits.append(current)
            meta = line.split("")
            if len(meta) != 4:
                current = None
                continue
            current = {
                "commit_hash": meta[0],
                "author": meta[1],
                "timestamp": meta[2],
                "title": meta[3],
                "files": [],
            }
            continue
        if current is None:
            continue
        file_path = _normalize_git_path(line)
        if not file_path or file_path.startswith('.pmai/'):
            continue
        if file_path not in current["files"]:
            current["files"].append(file_path)

    if current is not None:
        commits.append(current)
    return commits


def _get_git_commit_snapshot(commit_hash: str) -> Dict[str, Any]:
    if not commit_hash:
        return {
            "found": False,
            "commit_hash": "",
            "title": "",
            "author": "",
            "timestamp": "",
            "files": [],
            "stats": [],
        }

    meta = _run_git(["show", "-s", "--date=iso-strict", "--format=%H%x1f%an%x1f%ad%x1f%s", commit_hash])
    if not meta:
        return {
            "found": False,
            "commit_hash": commit_hash,
            "title": "",
            "author": "",
            "timestamp": "",
            "files": [],
            "stats": [],
        }

    meta_parts = meta.split("")
    files_output = _run_git(["show", "--pretty=format:", "--name-only", commit_hash]) or ""
    stats_output = _run_git(["show", "--stat", "--oneline", "--format=%h %s", commit_hash]) or ""
    files: List[str] = []
    for line in files_output.splitlines():
        file_path = _normalize_git_path(line)
        if not file_path or file_path.startswith('.pmai/'):
            continue
        if file_path not in files:
            files.append(file_path)
    stats = [line.rstrip() for line in stats_output.splitlines() if line.strip()]
    return {
        "found": True,
        "commit_hash": meta_parts[0] if len(meta_parts) > 0 else commit_hash,
        "author": meta_parts[1] if len(meta_parts) > 1 else "",
        "timestamp": meta_parts[2] if len(meta_parts) > 2 else "",
        "title": meta_parts[3] if len(meta_parts) > 3 else "",
        "files": files,
        "stats": stats,
    }


def get_status_snapshot() -> Dict[str, Any]:
    in_progress_tasks = list_tasks("in_progress")
    daily = get_daily_note()
    inbox = get_inbox_summary()
    pmai_commits = list_commits()[:3]
    git_status = get_git_worktree_status()
    git_recent = list_recent_git_commits(3)
    active_vision = get_active_vision()
    active_principles = list_active_principles()
    return {
        "project": describe_runtime(),
        "vision": active_vision,
        "principles": active_principles,
        "entrypoints": {
            "product": "aipmc brief product",
            "architecture": "aipmc brief architecture",
            "modules": "aipmc brief modules",
            "code": "aipmc code status",
        },
        "tasks": {
            "in_progress_count": len(in_progress_tasks),
            "in_progress": in_progress_tasks[:5],
        },
        "daily": daily,
        "inbox": {
            "counts": inbox.get("counts", {}),
            "recommended_actions": inbox.get("recommended_actions", [])[:5],
        },
        "recent_commits": {
            "pmai": pmai_commits,
            "git": git_recent,
        },
        "git": git_status,
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


def get_task(task_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if not row:
            raise KeyError(task_id)
        linked_commits = list_commits(task_id=task_id)
        approved_commits = [
            item
            for item in linked_commits
            if item["status"] in ("committed", "merged") and item["review_status"] == "approved"
        ]
        blocker_reasons: List[str] = []
        if not linked_commits:
            blocker_reasons.append("no_linked_commit")
        if linked_commits and not approved_commits:
            blocker_reasons.append("no_approved_commit")
        if any(item["test_status"] != "passed" for item in linked_commits):
            blocker_reasons.append("verification_incomplete")
        changed_files: List[str] = []
        for commit in linked_commits:
            for file_path in commit.get("files", []):
                if file_path not in changed_files:
                    changed_files.append(file_path)
        return {
            "task": {
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
            },
            "linked_commits": linked_commits,
            "links": list_links_for_entity(task_id),
            "changed_files": changed_files,
            "closure": {
                "linked_commit_count": len(linked_commits),
                "approved_commit_count": len(approved_commits),
                "can_mark_done": bool(approved_commits),
                "blocker_reasons": blocker_reasons,
            },
        }
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
                "files": _normalize_file_list(loads(row["files_json"], [])),
                "created_at": row["created_at"],
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def get_commit(commit_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM commits WHERE id = ?", (commit_id,)).fetchone()
        if not row:
            raise KeyError(commit_id)
        task = None
        decision = None
        if row["task_id"]:
            task_row = conn.execute("SELECT id, title, status, priority, phase FROM tasks WHERE id = ?", (row["task_id"],)).fetchone()
            if task_row:
                task = {
                    "id": task_row["id"],
                    "title": task_row["title"],
                    "status": task_row["status"],
                    "priority": task_row["priority"],
                    "phase": task_row["phase"],
                }
        if row["decision_id"]:
            decision_row = conn.execute("SELECT id, title, status, date FROM decisions WHERE id = ?", (row["decision_id"],)).fetchone()
            if decision_row:
                decision = {
                    "id": decision_row["id"],
                    "title": decision_row["title"],
                    "status": decision_row["status"],
                    "date": decision_row["date"],
                }
        commit = {
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
            "files": _normalize_file_list(loads(row["files_json"], [])),
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
        }
        return {
            "commit": commit,
            "linked_task": task,
            "linked_decision": decision,
            "links": list_links_for_entity(commit_id),
            "git": _get_git_commit_snapshot(row["commit_hash"]),
        }
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
            "files": _normalize_file_list(payload.get("files", []) or git_meta.get("files", [])),
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

        git_meta = infer_git_metadata() if payload.get("auto_git") else {}

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

        next_files = _normalize_file_list(loads(row["files_json"], []))
        if payload.get("files") is not None:
            next_files = _normalize_file_list(payload.get("files") or [])
        elif git_meta.get("files"):
            next_files = _normalize_file_list(git_meta.get("files") or [])

        next_branch = payload.get("branch") if payload.get("branch") is not None else row["branch"]
        if git_meta.get("branch") and payload.get("branch") is None:
            next_branch = git_meta.get("branch", "")

        next_commit_hash = payload.get("commit_hash") if payload.get("commit_hash") is not None else row["commit_hash"]
        if git_meta.get("commit_hash") and payload.get("commit_hash") is None:
            next_commit_hash = git_meta.get("commit_hash", "")

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
                next_branch,
                next_commit_hash,
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
            "files": _normalize_file_list(loads(updated["files_json"], [])),
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


def get_decision(decision_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM decisions WHERE id = ?", (decision_id,)).fetchone()
        if not row:
            raise KeyError(decision_id)
        related_task_ids = loads(row["related_tasks_json"], [])
        related_tasks: List[Dict[str, Any]] = []
        for task_id in related_task_ids:
            task_row = conn.execute(
                "SELECT id, title, status, priority, phase FROM tasks WHERE id = ?",
                (task_id,),
            ).fetchone()
            if task_row:
                related_tasks.append(
                    {
                        "id": task_row["id"],
                        "title": task_row["title"],
                        "status": task_row["status"],
                        "priority": task_row["priority"],
                        "phase": task_row["phase"],
                    }
                )
        linked_commits = list_commits(decision_id=decision_id)
        return {
            "decision": {
                "id": row["id"],
                "title": row["title"],
                "date": row["date"],
                "status": row["status"],
                "background": row["background"],
                "decision": row["decision_text"],
                "impact": loads(row["impact_json"], []),
                "alternatives": loads(row["alternatives_json"], []),
                "related_tasks": related_task_ids,
                "updates_canon": bool(row["updates_canon"]),
            },
            "linked_tasks": related_tasks,
            "linked_commits": linked_commits,
            "links": list_links_for_entity(decision_id),
        }
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


def get_idea(idea_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM ideas WHERE id = ?", (idea_id,)).fetchone()
        if not row:
            raise KeyError(idea_id)
        return {
            "id": row["id"],
            "title": row["title"],
            "summary": row["summary"],
            "impact": row["impact"],
            "source": row["source"],
            "status": row["status"],
            "canon_conflict": bool(row["canon_conflict"]),
            "created_at": row["created_at"],
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


def list_visions(status: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM visions"
        params: List[Any] = []
        if status:
            query += " WHERE status = ?"
            params.append(status)
        query += " ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, updated_at DESC, id DESC"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "status": row["status"],
                "horizon": row["horizon"],
                "created_at": row["created_at"],
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def get_vision(vision_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM visions WHERE id = ?", (vision_id,)).fetchone()
        if not row:
            raise KeyError(vision_id)
        return {
            "id": row["id"],
            "title": row["title"],
            "summary": row["summary"],
            "status": row["status"],
            "horizon": row["horizon"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "links": list_links_for_entity(vision_id),
        }
    finally:
        conn.close()


def create_vision(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        vision_id = slug("vision")
        created_at = now_iso()
        status = payload.get("status", "active")
        if status == "active":
            conn.execute("UPDATE visions SET status = 'archived', updated_at = ? WHERE status = 'active'", (created_at,))
        conn.execute(
            """
            INSERT INTO visions (id, title, summary, status, horizon, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                vision_id,
                payload["title"],
                payload.get("summary", ""),
                status,
                payload.get("horizon", "long_term"),
                created_at,
                created_at,
            ),
        )
        conn.commit()
        return get_vision(vision_id)
    finally:
        conn.close()


def update_vision(vision_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM visions WHERE id = ?", (vision_id,)).fetchone()
        if not row:
            raise KeyError(vision_id)
        next_status = payload.get("status") if payload.get("status") is not None else row["status"]
        updated_at = now_iso()
        if next_status == "active":
            conn.execute(
                "UPDATE visions SET status = 'archived', updated_at = ? WHERE status = 'active' AND id != ?",
                (updated_at, vision_id),
            )
        conn.execute(
            """
            UPDATE visions
            SET title = ?, summary = ?, status = ?, horizon = ?, updated_at = ?
            WHERE id = ?
            """,
            (
                payload.get("title") if payload.get("title") is not None else row["title"],
                payload.get("summary") if payload.get("summary") is not None else row["summary"],
                next_status,
                payload.get("horizon") if payload.get("horizon") is not None else row["horizon"],
                updated_at,
                vision_id,
            ),
        )
        conn.commit()
        return get_vision(vision_id)
    finally:
        conn.close()


def get_active_vision() -> Optional[Dict[str, Any]]:
    visions = list_visions("active")
    return visions[0] if visions else None



def list_principles(status: Optional[str] = None, kind: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM principles"
        params: List[Any] = []
        conditions: List[str] = []
        if status:
            conditions.append("status = ?")
            params.append(status)
        if kind:
            conditions.append("kind = ?")
            params.append(kind)
        if conditions:
            query += " WHERE " + " AND ".join(conditions)
        query += " ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, updated_at DESC, id DESC"
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "kind": row["kind"],
                "status": row["status"],
                "created_at": row["created_at"],
                "updated_at": row["updated_at"],
            }
            for row in rows
        ]
    finally:
        conn.close()


def get_principle(principle_id: str) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM principles WHERE id = ?", (principle_id,)).fetchone()
        if not row:
            raise KeyError(principle_id)
        return {
            "id": row["id"],
            "title": row["title"],
            "summary": row["summary"],
            "kind": row["kind"],
            "status": row["status"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "links": list_links_for_entity(principle_id),
        }
    finally:
        conn.close()


def create_principle(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        principle_id = slug("principle")
        created_at = now_iso()
        conn.execute(
            """
            INSERT INTO principles (id, title, summary, kind, status, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                principle_id,
                payload["title"],
                payload.get("summary", ""),
                payload.get("kind", "governance"),
                payload.get("status", "active"),
                created_at,
                created_at,
            ),
        )
        conn.commit()
        return get_principle(principle_id)
    finally:
        conn.close()


def update_principle(principle_id: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        row = conn.execute("SELECT * FROM principles WHERE id = ?", (principle_id,)).fetchone()
        if not row:
            raise KeyError(principle_id)
        conn.execute(
            """
            UPDATE principles
            SET title = ?, summary = ?, kind = ?, status = ?, updated_at = ?
            WHERE id = ?
            """,
            (
                payload.get("title") if payload.get("title") is not None else row["title"],
                payload.get("summary") if payload.get("summary") is not None else row["summary"],
                payload.get("kind") if payload.get("kind") is not None else row["kind"],
                payload.get("status") if payload.get("status") is not None else row["status"],
                now_iso(),
                principle_id,
            ),
        )
        conn.commit()
        return get_principle(principle_id)
    finally:
        conn.close()


def list_active_principles(limit: int = 5) -> List[Dict[str, Any]]:
    return list_principles(status="active")[:limit]



def _serialize_link(row: sqlite3.Row) -> Dict[str, Any]:
    return {
        "id": row["id"],
        "source_type": row["source_type"],
        "source_id": row["source_id"],
        "relation": row["relation"],
        "target_type": row["target_type"],
        "target_id": row["target_id"],
        "note": row["note"],
        "created_at": row["created_at"],
    }


def list_links(source_id: Optional[str] = None, target_id: Optional[str] = None, relation: Optional[str] = None) -> List[Dict[str, Any]]:
    conn = get_connection()
    try:
        query = "SELECT * FROM links"
        params: List[Any] = []
        conditions: List[str] = []
        if source_id:
            conditions.append("source_id = ?")
            params.append(source_id)
        if target_id:
            conditions.append("target_id = ?")
            params.append(target_id)
        if relation:
            conditions.append("relation = ?")
            params.append(relation)
        if conditions:
            query += " WHERE " + " AND ".join(conditions)
        query += " ORDER BY created_at DESC, id DESC"
        rows = conn.execute(query, params).fetchall()
        return [_serialize_link(row) for row in rows]
    finally:
        conn.close()


def list_links_for_entity(entity_id: str) -> Dict[str, List[Dict[str, Any]]]:
    conn = get_connection()
    try:
        outgoing = conn.execute(
            "SELECT * FROM links WHERE source_id = ? ORDER BY created_at DESC, id DESC",
            (entity_id,),
        ).fetchall()
        incoming = conn.execute(
            "SELECT * FROM links WHERE target_id = ? ORDER BY created_at DESC, id DESC",
            (entity_id,),
        ).fetchall()
        return {
            "outgoing": [_serialize_link(row) for row in outgoing],
            "incoming": [_serialize_link(row) for row in incoming],
        }
    finally:
        conn.close()


def create_link(payload: Dict[str, Any]) -> Dict[str, Any]:
    conn = get_connection()
    try:
        link_id = slug("link")
        row = {
            "id": link_id,
            "source_type": payload["source_type"],
            "source_id": payload["source_id"],
            "relation": payload["relation"],
            "target_type": payload["target_type"],
            "target_id": payload["target_id"],
            "note": payload.get("note", ""),
            "created_at": now_iso(),
        }
        conn.execute(
            """
            INSERT INTO links (id, source_type, source_id, relation, target_type, target_id, note, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["source_type"],
                row["source_id"],
                row["relation"],
                row["target_type"],
                row["target_id"],
                row["note"],
                row["created_at"],
            ),
        )
        conn.commit()
        return row
    finally:
        conn.close()


def delete_link(link_id: str) -> bool:
    conn = get_connection()
    try:
        cur = conn.execute("DELETE FROM links WHERE id = ?", (link_id,))
        conn.commit()
        return cur.rowcount > 0
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


def _module_guess_from_task(task: Dict[str, Any]) -> str:
    raw_title = task.get("title") or ""
    title = raw_title.lower()
    phase = (task.get("phase") or "").lower()
    analytics_keywords = ["埋点", "数据", "画像", "智能", "assessment", "feature", "analytics"]
    ai_keywords = ["AI", "ai", "教练", "对话", "元认知", "coach"]
    review_keywords = ["复盘", "review", "remediation"]
    paper_keywords = ["题目", "试卷", "paper", "question"]
    if any(keyword in raw_title for keyword in analytics_keywords):
        return "analytics"
    if any(keyword in raw_title for keyword in ai_keywords):
        return "ai_core"
    if any(keyword in raw_title for keyword in review_keywords):
        return "review"
    if any(keyword in raw_title for keyword in paper_keywords):
        return "papers"
    if phase in {"analytics", "review", "papers", "ai_core"}:
        return phase
    if phase:
        return f"phase:{phase}"
    return "general"


def build_brief(view: str) -> Dict[str, Any]:
    vision = get_active_vision()
    principles = list_active_principles(limit=8)
    canon = fetch_canon()
    tasks = list_tasks()
    in_progress = [task for task in tasks if task["status"] == "in_progress"]
    commits = list_commits()[:5]
    daily = get_daily_note()
    links = list_links()
    docs = list_doc_records(status="active")
    git_status = get_git_worktree_status()
    decisions = list_decisions()
    accepted_decisions = [item for item in decisions if item["status"] == "accepted"][:5]

    if view == "product":
        return {
            "view": "product",
            "goal": vision,
            "principles": principles,
            "current_stage": {
                "engineering_focus": canon.get("engineering_focus", ""),
                "architecture": canon.get("architecture", ""),
            },
            "active_workstreams": [
                {
                    "id": task["id"],
                    "title": task["title"],
                    "priority": task["priority"],
                    "phase": task["phase"],
                }
                for task in in_progress[:6]
            ],
            "accepted_decisions": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "date": item["date"],
                }
                for item in accepted_decisions
            ],
            "today": {
                "completed": daily.get("completed", [])[:5],
                "risks": daily.get("risks", [])[:5],
                "next": daily.get("next", [])[:5],
            },
        }

    if view == "architecture":
        return {
            "view": "architecture",
            "goal": vision,
            "principles": principles,
            "accepted_decisions": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "date": item["date"],
                    "updates_canon": item["updates_canon"],
                }
                for item in accepted_decisions
            ],
            "system_layers": [
                {
                    "name": "governance",
                    "objects": ["vision", "principle", "decision", "task", "commit", "link"],
                },
                {
                    "name": "runtime_code",
                    "objects": ["code status", "code diff", "code recent"],
                },
                {
                    "name": "project_runtime",
                    "objects": ["daily", "inbox", "canon", "docs"],
                },
            ],
            "current_boundaries": {
                "canon": canon,
                "git": {
                    "branch": git_status.get("branch"),
                    "dirty": git_status.get("dirty"),
                    "changed_files_count": git_status.get("changed_files_count"),
                },
            },
            "recent_evidence": {
                "commits": commits,
                "links": links[:8],
                "active_docs": docs[:8],
            },
        }

    if view == "modules":
        grouped: Dict[str, List[Dict[str, Any]]] = {}
        for task in in_progress:
            grouped.setdefault(_module_guess_from_task(task), []).append(task)
        modules = []
        for name, items in sorted(grouped.items(), key=lambda pair: pair[0]):
            task_ids = {task["id"] for task in items}
            module_commits = [commit for commit in commits if commit.get("task_id") in task_ids]
            modules.append(
                {
                    "module": name,
                    "tasks": [
                        {
                            "id": task["id"],
                            "title": task["title"],
                            "priority": task["priority"],
                            "status": task["status"],
                        }
                        for task in items
                    ],
                    "recent_commits": [
                        {
                            "id": commit["id"],
                            "title": commit["title"],
                            "task_id": commit.get("task_id"),
                        }
                        for commit in module_commits[:3]
                    ],
                }
            )
        return {
            "view": "modules",
            "goal": vision,
            "accepted_decisions": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "date": item["date"],
                }
                for item in accepted_decisions
            ],
            "modules": modules,
            "recent_commits": commits,
            "links": links[:10],
        }

    raise KeyError(view)


def get_inbox_summary() -> Dict[str, Any]:
    canon = fetch_canon()
    tasks = list_tasks()
    decisions = list_decisions()
    commits = list_commits()
    doc_audit = audit_docs()
    canon_related_decisions = set(canon.get("related_decisions", []))

    proposed_decisions = [item for item in decisions if item["status"] == "proposed"]
    canon_followups = [
        item
        for item in decisions
        if item["status"] == "accepted"
        and item["updates_canon"]
        and item["id"] not in canon_related_decisions
    ]
    review_commits = [
        item
        for item in commits
        if item["status"] in ("committed", "merged") and item["review_status"] != "approved"
    ]
    verification_gaps = [
        item
        for item in commits
        if item["status"] in ("committed", "merged")
        and (item["test_status"] != "passed" or item["review_status"] != "approved")
    ]
    task_closure_blockers: List[Dict[str, Any]] = []
    for task in tasks:
        if task["status"] not in ("in_progress", "blocked"):
            continue
        linked_commits = [item for item in commits if item["task_id"] == task["id"]]
        approved_linked_commits = [
            item
            for item in linked_commits
            if item["status"] in ("committed", "merged") and item["review_status"] == "approved"
        ]
        blocker_reasons: List[str] = []
        if not linked_commits:
            blocker_reasons.append("no_linked_commit")
        if linked_commits and not approved_linked_commits:
            blocker_reasons.append("no_approved_commit")
        if any(item["test_status"] != "passed" for item in linked_commits):
            blocker_reasons.append("verification_incomplete")
        if blocker_reasons:
            task_closure_blockers.append(
                {
                    "id": task["id"],
                    "title": task["title"],
                    "status": task["status"],
                    "priority": task["priority"],
                    "phase": task["phase"],
                    "reasons": blocker_reasons,
                    "linked_commit_ids": [item["id"] for item in linked_commits],
                }
            )
    blocking_doc_issues: List[Dict[str, Any]] = []
    for path in doc_audit.get("obsolete_without_replacement", []):
        blocking_doc_issues.append(
            {
                "type": "obsolete_without_replacement",
                "path": path,
            }
        )
    for path in doc_audit.get("invalid_truth_records", []):
        blocking_doc_issues.append(
            {
                "type": "invalid_truth_records",
                "path": path,
            }
        )

    recommended_actions: List[Dict[str, Any]] = []
    for item in proposed_decisions[:3]:
        recommended_actions.append(
            {
                "kind": "decision_review",
                "priority": "high",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc decision review --id {item['id']} --status accepted",
                "reason": "Proposed decisions should be explicitly accepted or rejected before downstream work continues.",
            }
        )
    for item in canon_followups[:3]:
        recommended_actions.append(
            {
                "kind": "canon_followup",
                "priority": "high",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc canon update --decision-id {item['id']}",
                "reason": "Accepted decisions marked as canon-affecting should be reflected in canon explicitly.",
            }
        )
    for item in review_commits[:3]:
        recommended_actions.append(
            {
                "kind": "commit_review",
                "priority": "medium",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc commit update --id {item['id']} --review-status approved",
                "reason": "Committed changes should be reviewed so related tasks can close without manual guesswork.",
            }
        )
    for item in verification_gaps[:3]:
        recommended_actions.append(
            {
                "kind": "verification_gap",
                "priority": "medium",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc commit update --id {item['id']} --test-status passed --review-status approved",
                "reason": "Committed changes without passing verification or review should not be treated as closure-ready.",
            }
        )
    for item in task_closure_blockers[:3]:
        recommended_actions.append(
            {
                "kind": "task_closure_blocker",
                "priority": "medium",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc commit list --task-id {item['id']}",
                "reason": f"Task remains closure-blocked because: {', '.join(item['reasons'])}.",
            }
        )
    for item in blocking_doc_issues[:3]:
        recommended_actions.append(
            {
                "kind": "doc_governance",
                "priority": "medium",
                "target_id": item["path"],
                "title": item["path"],
                "command": "aipmc docs audit",
                "reason": f"Document issue `{item['type']}` needs explicit cleanup to keep source-of-truth status reliable.",
            }
        )

    return {
        "canon": {
            "updated_at": canon.get("updated_at"),
            "related_decisions_count": len(canon_related_decisions),
        },
        "counts": {
            "proposed_decisions": len(proposed_decisions),
            "canon_followups": len(canon_followups),
            "review_commits": len(review_commits),
            "verification_gaps": len(verification_gaps),
            "task_closure_blockers": len(task_closure_blockers),
            "doc_issues": len(blocking_doc_issues),
            "total": (
                len(proposed_decisions)
                + len(canon_followups)
                + len(review_commits)
                + len(verification_gaps)
                + len(task_closure_blockers)
                + len(blocking_doc_issues)
            ),
        },
        "pending_items": {
            "proposed_decisions": proposed_decisions,
            "canon_followups": canon_followups,
            "review_commits": review_commits,
            "verification_gaps": verification_gaps,
            "task_closure_blockers": task_closure_blockers,
            "doc_issues": blocking_doc_issues,
        },
        "recommended_actions": recommended_actions,
    }
