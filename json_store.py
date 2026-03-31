from __future__ import annotations

import json
from copy import deepcopy
from pathlib import Path
from typing import Any, Optional

try:
    from .store import get_db_path, now_iso, slug, today
except ImportError:
    from store import get_db_path, now_iso, slug, today


def default_state() -> dict[str, Any]:
    return {
        "meta": {},
        "canon": None,
        "decisions": [],
        "tasks": [],
        "ideas": [],
        "doc_records": [],
        "daily_notes": {},
        "commits": [],
    }


def db_path() -> Path:
    return get_db_path(create_parent=True)


def load_state() -> dict[str, Any]:
    path = db_path()
    if not path.exists():
        return default_state()
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return default_state()


def save_state(state: dict[str, Any]) -> None:
    path = db_path()
    path.write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")


def bootstrap_state(payload: dict[str, Any]) -> None:
    state = default_state()
    state.update(payload)
    save_state(state)


def fetch_canon() -> dict[str, Any]:
    canon = load_state().get("canon")
    if not canon:
        raise FileNotFoundError("Canon not found. Run `planai init` first.")
    return canon


def update_canon(payload: dict[str, Any]) -> dict[str, Any]:
    state = load_state()
    canon = deepcopy(state["canon"])
    decisions = {item["id"]: item for item in state["decisions"]}
    decision = decisions.get(payload["decision_id"])
    if not decision or decision["status"] != "accepted":
        raise KeyError(payload["decision_id"])
    for key in ["product_goal", "engineering_focus", "architecture"]:
        if payload.get(key):
            canon[key] = payload[key]
    for item in payload.get("add_scope", []):
        if item and item not in canon["version_scope"]:
            canon["version_scope"].append(item)
    for item in payload.get("add_avoid", []):
        if item and item not in canon["avoid_now"]:
            canon["avoid_now"].append(item)
    if payload["decision_id"] not in canon["related_decisions"]:
        canon["related_decisions"].append(payload["decision_id"])
    canon["updated_at"] = today()
    state["canon"] = canon
    save_state(state)
    return canon


def list_collection(name: str, status: Optional[str] = None) -> list[dict[str, Any]]:
    rows = deepcopy(load_state()[name])
    if status:
        rows = [row for row in rows if row.get("status") == status]
    return rows


def list_tasks() -> list[dict[str, Any]]:
    return list_collection("tasks")


def list_commits(status: Optional[str] = None) -> list[dict[str, Any]]:
    return list_collection("commits", status=status)


def list_decisions() -> list[dict[str, Any]]:
    return list_collection("decisions")


def list_ideas(status: Optional[str] = None) -> list[dict[str, Any]]:
    return list_collection("ideas", status=status)


def _upsert_item(name: str, item: dict[str, Any], key: str = "id") -> dict[str, Any]:
    state = load_state()
    rows = state[name]
    for index, row in enumerate(rows):
        if row[key] == item[key]:
            rows[index] = item
            save_state(state)
            return item
    rows.append(item)
    save_state(state)
    return item


def create_task(payload: dict[str, Any]) -> dict[str, Any]:
    return _upsert_item(
        "tasks",
        {
            "id": slug("task"),
            "title": payload["title"],
            "status": payload.get("status", "todo"),
            "priority": payload.get("priority", "P1"),
            "phase": payload.get("phase", "general"),
            "acceptance": payload.get("acceptance", []),
            "related_docs": [],
            "related_decisions": [],
            "last_note": "",
            "updated_at": today(),
        },
    )


def update_task(task_id: str, status: str, note: str = "") -> dict[str, Any]:
    state = load_state()
    for row in state["tasks"]:
        if row["id"] == task_id:
            row["status"] = status
            row["last_note"] = note
            row["updated_at"] = today()
            save_state(state)
            return row
    raise KeyError(task_id)


def create_commit(payload: dict[str, Any]) -> dict[str, Any]:
    row = {
        "id": slug("commit"),
        "title": payload["title"],
        "summary": payload.get("summary", ""),
        "branch": payload.get("branch", ""),
        "commit_hash": payload.get("commit_hash", ""),
        "task_id": payload.get("task_id"),
        "decision_id": payload.get("decision_id"),
        "status": payload.get("status", "draft"),
        "test_status": payload.get("test_status", "not_run"),
        "review_status": payload.get("review_status", "pending"),
        "files": payload.get("files", []),
        "created_at": now_iso(),
        "updated_at": now_iso(),
    }
    return _upsert_item("commits", row)


def update_commit(commit_id: str, payload: dict[str, Any]) -> dict[str, Any]:
    state = load_state()
    for row in state["commits"]:
        if row["id"] != commit_id:
            continue
        for key in ["title", "summary", "branch", "commit_hash", "task_id", "decision_id", "status", "test_status", "review_status", "files"]:
            if key in payload and payload[key] is not None:
                row[key] = payload[key]
        row["updated_at"] = now_iso()
        save_state(state)
        return row
    raise KeyError(commit_id)


def create_decision(payload: dict[str, Any]) -> dict[str, Any]:
    return _upsert_item(
        "decisions",
        {
            "id": slug("decision"),
            "title": payload["title"],
            "date": today(),
            "status": payload.get("status", "proposed"),
            "background": payload["background"],
            "decision": payload["decision"],
            "impact": payload.get("impact", []),
            "alternatives": payload.get("alternatives", []),
            "related_tasks": payload.get("related_tasks", []),
            "updates_canon": bool(payload.get("updates_canon", False)),
        },
    )


def update_decision_status(decision_id: str, status: str) -> dict[str, Any]:
    state = load_state()
    for row in state["decisions"]:
        if row["id"] == decision_id:
            row["status"] = status
            save_state(state)
            return row
    raise KeyError(decision_id)


def create_idea(payload: dict[str, Any]) -> dict[str, Any]:
    return _upsert_item(
        "ideas",
        {
            "id": slug("idea"),
            "title": payload["title"],
            "summary": payload["summary"],
            "impact": payload.get("impact", ""),
            "source": payload.get("source", "web"),
            "status": "inbox",
            "canon_conflict": bool(payload.get("canon_conflict", False)),
            "created_at": now_iso(),
        },
    )


def review_idea(idea_id: str, status: str, note: str = "") -> dict[str, Any]:
    state = load_state()
    for row in state["ideas"]:
        if row["id"] == idea_id:
            row["status"] = status
            if note:
                row["summary"] = (row["summary"] or "").rstrip() + f"\n\n[review-note] {note}"
            save_state(state)
            return row
    raise KeyError(idea_id)


def list_doc_records(status: Optional[str] = None, layer: Optional[str] = None) -> list[dict[str, Any]]:
    rows = deepcopy(load_state()["doc_records"])
    if status:
        rows = [row for row in rows if row["status"] == status]
    if layer:
        rows = [row for row in rows if row["layer"] == layer]
    return rows


def update_doc_record(payload: dict[str, Any]) -> dict[str, Any]:
    state = load_state()
    rows = state["doc_records"]
    for index, row in enumerate(rows):
        if row["path"] == payload["path"]:
            rows[index] = {**row, **{k: v for k, v in payload.items() if v is not None}}
            save_state(state)
            return rows[index]
    if not payload.get("create"):
        raise KeyError(payload["path"])
    row = {
        "path": payload["path"],
        "type": payload.get("type", "unknown"),
        "status": payload.get("status", "draft"),
        "layer": payload.get("layer", "exploration"),
        "source_of_truth": bool(payload.get("source_of_truth", False)),
        "last_reviewed": payload.get("last_reviewed") or today(),
        "superseded_by": payload.get("superseded_by"),
    }
    rows.append(row)
    save_state(state)
    return row


def audit_docs() -> dict[str, Any]:
    rows = list_doc_records()
    return {
        "database": str(db_path()),
        "total_records": len(rows),
        "active_records": len([row for row in rows if row["status"] == "active"]),
        "source_of_truth_records": len([row for row in rows if row["source_of_truth"]]),
        "obsolete_without_replacement": [row["path"] for row in rows if row["status"] == "obsolete" and not row.get("superseded_by")],
        "invalid_truth_records": [row["path"] for row in rows if row["status"] in {"archived", "obsolete"} and row["source_of_truth"]],
    }


def get_daily_note(note_date: Optional[str] = None) -> dict[str, Any]:
    target = note_date or today()
    row = load_state()["daily_notes"].get(target)
    if not row:
        return {"note_date": target, "completed": [], "problems": [], "risks": [], "next": [], "updated_at": None}
    return row


def list_daily_notes(limit: int = 30) -> list[dict[str, Any]]:
    rows = list(load_state()["daily_notes"].values())
    rows.sort(key=lambda item: item["note_date"], reverse=True)
    return rows[:limit]


def replace_daily_note(payload: dict[str, Any], note_date: Optional[str] = None) -> dict[str, Any]:
    target = note_date or today()
    state = load_state()
    row = {"note_date": target, "completed": payload.get("completed", []), "problems": payload.get("problems", []), "risks": payload.get("risks", []), "next": payload.get("next", []), "updated_at": now_iso()}
    state["daily_notes"][target] = row
    save_state(state)
    return row


def append_daily_note(payload: dict[str, Any], note_date: Optional[str] = None) -> dict[str, Any]:
    current = get_daily_note(note_date)
    return replace_daily_note(
        {
            "completed": current["completed"] + payload.get("completed", []),
            "problems": current["problems"] + payload.get("problems", []),
            "risks": current["risks"] + payload.get("risks", []),
            "next": current["next"] + payload.get("next", []),
        },
        note_date,
    )


def get_dashboard_summary() -> dict[str, Any]:
    state = load_state()
    return {
        "canon": state["canon"],
        "task_summary": {"total": len(state["tasks"]), "in_progress": len([row for row in state["tasks"] if row["status"] == "in_progress"])},
        "decision_summary": {"total": len(state["decisions"]), "accepted": len([row for row in state["decisions"] if row["status"] == "accepted"])},
        "commit_summary": {"total": len(state["commits"]), "merged": len([row for row in state["commits"] if row["status"] == "merged"])},
        "idea_summary": {"total": len(state["ideas"])},
        "doc_summary": audit_docs(),
        "daily": get_daily_note(),
    }
