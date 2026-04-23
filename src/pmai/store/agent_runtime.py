from __future__ import annotations

from typing import Any, Dict, List

from .context_runtime import build_context_pack, build_next_action_packet, build_progress_packet
from .decisions import list_decisions
from .docs import list_doc_records
from .ideas import list_ideas
from .plans import list_plans
from .roadmaps import list_roadmaps
from .tasks import list_tasks


def build_agent_start_packet() -> Dict[str, Any]:
    context = build_context_pack()
    progress = build_progress_packet()
    next_packet = build_next_action_packet()
    mainline = context.get("mainline", {})

    return {
        "role": "ai_start",
        "message": "Use this before coding. Do not create duplicate PMAI tasks/docs.",
        "rules": [
            "Use the current task/doc context before coding.",
            "Before adding a task or doc: `aipmc search \"<topic>\"`.",
            "If work already exists: `aipmc task note --id <task-id> --content \"...\"`.",
        ],
        "current_focus": {
            "roadmap": _compact_named(mainline.get("roadmap")),
            "plan": _compact_named(mainline.get("plan")),
            "task": _compact_task(mainline.get("task")),
            "next_command": (next_packet.get("next_action") or {}).get("command"),
        },
        "existing_context": {
            "source_of_truth_docs": context.get("project", {}).get("source_of_truth_docs", [])[:3],
            "in_progress_tasks": [_compact_task(task) for task in mainline.get("in_progress_tasks", [])[:3]],
        },
        "health": {
            "git_dirty": progress.get("health", {}).get("git_dirty"),
            "in_progress_count": progress.get("health", {}).get("in_progress_count"),
        },
    }


def search_project_context(query: str, limit: int = 8) -> Dict[str, Any]:
    terms = _terms(query)
    if not terms:
        raise ValueError("search query cannot be empty")

    results = []
    results.extend(_search_items("task", list_tasks(), terms, ["title", "status", "phase", "last_note"]))
    results.extend(_search_items("doc", list_doc_records(), terms, ["path", "type", "status", "layer"]))
    results.extend(_search_items("decision", list_decisions(), terms, ["title", "status", "background", "decision"]))
    results.extend(_search_items("idea", list_ideas(), terms, ["title", "summary", "current_summary", "status"]))
    results.extend(_search_items("plan", list_plans(), terms, ["title", "goal", "status"]))
    results.extend(_search_items("roadmap", list_roadmaps(), terms, ["title", "status", "priority"]))
    results.sort(key=lambda item: (-item["score"], item["type"], item.get("title") or item.get("id") or ""))

    return {
        "query": query,
        "count": len(results),
        "results": results[: max(limit, 1)],
        "next_actions": [
            "If a result matches, inspect it before creating anything new.",
            "Use `aipmc task note --id <task-id> --content \"...\"` to continue existing work.",
            "Only use `task add` or docs creation when these results do not fit.",
        ],
    }


def _terms(query: str) -> List[str]:
    return [term.lower() for term in query.replace("_", " ").replace("-", " ").split() if term.strip()]


def _compact_task(task: Dict[str, Any] | None) -> Dict[str, Any] | None:
    if not task:
        return None
    task_id = task.get("id")
    return {
        "id": task_id,
        "title": task.get("title"),
        "status": task.get("status"),
        "priority": task.get("priority"),
        "command": f"aipmc task show --id {task_id}" if task_id else None,
    }


def _compact_named(item: Dict[str, Any] | None) -> Dict[str, Any] | None:
    if not item:
        return None
    item_id = item.get("id")
    return {
        "id": item_id,
        "title": item.get("title"),
        "status": item.get("status"),
    }


def _search_items(item_type: str, items: List[Dict[str, Any]], terms: List[str], fields: List[str]) -> List[Dict[str, Any]]:
    matches: List[Dict[str, Any]] = []
    for item in items:
        haystack_parts = [str(item.get(field, "")) for field in fields]
        haystack = " ".join(haystack_parts).lower()
        score = sum(1 for term in terms if term in haystack)
        if score <= 0:
            continue
        matches.append(_serialize_search_hit(item_type, item, score))
    return matches


def _serialize_search_hit(item_type: str, item: Dict[str, Any], score: int) -> Dict[str, Any]:
    hit = {
        "type": item_type,
        "id": item.get("id") or item.get("path"),
        "title": item.get("title") or item.get("path"),
        "status": item.get("status"),
        "score": score,
    }
    if item_type == "task":
        hit["command"] = f"aipmc task show --id {item['id']}"
    elif item_type == "doc":
        hit["command"] = f"aipmc docs update --path {item['path']}"
        hit["layer"] = item.get("layer")
        hit["source_of_truth"] = item.get("source_of_truth")
    elif item_type == "decision":
        hit["command"] = f"aipmc decision show --id {item['id']}"
    elif item_type == "idea":
        hit["command"] = f"aipmc idea show --id {item['id']}"
    elif item_type == "plan":
        hit["command"] = f"aipmc plan show --id {item['id']}"
    elif item_type == "roadmap":
        hit["command"] = f"aipmc roadmap show --id {item['id']}"
    return hit
