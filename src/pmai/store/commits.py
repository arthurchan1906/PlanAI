from __future__ import annotations

import json
from datetime import datetime
from typing import Any, Dict, List, Optional
from uuid import uuid4

from .db import get_connection
from .git import _normalize_file_list, infer_git_metadata, _get_git_commit_snapshot
from .links import list_links_for_entity


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
    try:
        return json.loads(value)
    except (json.JSONDecodeError, TypeError):
        return default


def list_commits(
    status: Optional[str] = None,
    task_id: Optional[str] = None,
    decision_id: Optional[str] = None,
    since: Optional[str] = None,
    limit: Optional[int] = None,
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
        if since:
            if since == "today":
                since = today()
            where_clauses.append("created_at >= ?")
            params.append(since)
        if where_clauses:
            query += " WHERE " + " AND ".join(where_clauses)
        query += " ORDER BY created_at DESC, id DESC"
        if limit is not None and limit > 0:
            query += " LIMIT ?"
            params.append(limit)
        rows = conn.execute(query, params).fetchall()
        return [
            {
                "id": row["id"],
                "title": row["title"],
                "summary": row["summary"],
                "evidence_summary": row["evidence_summary"] or "",
                "review_notes": row["review_notes"] or "",
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
            "evidence_summary": row["evidence_summary"] or "",
            "review_notes": row["review_notes"] or "",
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
            "evidence_summary": payload.get("evidence_summary", ""),
            "review_notes": payload.get("review_notes", ""),
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
                evidence_summary, review_notes, status, test_status, review_status, files_json, created_at, updated_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["id"],
                row["title"],
                row["summary"],
                row["branch"],
                row["commit_hash"],
                row["task_id"],
                row["decision_id"],
                row["evidence_summary"],
                row["review_notes"],
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
                evidence_summary = ?, review_notes = ?, status = ?, test_status = ?, review_status = ?, files_json = ?, updated_at = ?
            WHERE id = ?
            """,
            (
                payload.get("title") if payload.get("title") is not None else row["title"],
                payload.get("summary") if payload.get("summary") is not None else row["summary"],
                next_branch,
                next_commit_hash,
                next_task_id,
                next_decision_id,
                payload.get("evidence_summary") if payload.get("evidence_summary") is not None else row["evidence_summary"],
                payload.get("review_notes") if payload.get("review_notes") is not None else row["review_notes"],
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
            "evidence_summary": updated["evidence_summary"] or "",
            "review_notes": updated["review_notes"] or "",
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
