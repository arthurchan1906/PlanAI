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
    next_action = next_packet.get("next_action") or {}
    active_task = mainline.get("task") or {}
    active_plan = mainline.get("plan") or {}

    return {
        "role": "ai_start",
        "message": "Use this before coding. Reuse existing PMAI tasks/plans/decisions/docs before creating new ones.",
        "rules": [
            "Use the current task/doc context before coding.",
            "Before adding a task, plan, decision, or doc: `aipmc search \"<topic>\"`.",
            "If related work already exists: prefer `show`, `update`, or `task note` instead of creating a new record.",
        ],
        "current_focus": {
            "roadmap": _compact_named(mainline.get("roadmap")),
            "plan": _compact_named(mainline.get("plan")),
            "task": _compact_task(mainline.get("task")),
            "next_command": next_action.get("command"),
        },
        "existing_context": {
            "source_of_truth_docs": context.get("project", {}).get("source_of_truth_docs", [])[:3],
            "in_progress_tasks": [_compact_task(task) for task in mainline.get("in_progress_tasks", [])[:3]],
        },
        "health": {
            "git_dirty": progress.get("health", {}).get("git_dirty"),
            "in_progress_count": progress.get("health", {}).get("in_progress_count"),
        },
        "recommended_flow": [
            {
                "when": "Before coding or creating anything new",
                "command": "aipmc start",
            },
            {
                "when": "If the current work topic is not obvious",
                "command": "aipmc search \"<topic>\"",
            },
            {
                "when": "If matching work already exists",
                "command": active_task and f"aipmc task show --id {active_task['id']}" or active_plan and f"aipmc plan show --id {active_plan['id']}" or "aipmc next",
            },
            {
                "when": "Only after reuse checks fail",
                "command": "aipmc task add ...  or  aipmc plan add ...",
            },
        ],
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
            "Prefer `task show`, `plan show`, or `decision show` before using any `add` command.",
            "Use `aipmc task note --id <task-id> --content \"...\"` to continue existing work.",
            "Only create a new record when the existing search results clearly do not fit.",
        ],
        "recommended_commands": _build_search_recommended_commands(results[: max(limit, 1)]),
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


def _build_search_recommended_commands(results: List[Dict[str, Any]]) -> List[Dict[str, str]]:
    if not results:
        return [
            {
                "command": "aipmc next",
                "reason": "No clear existing match was found; refresh the mainline recommendation before creating anything.",
            }
        ]

    primary = results[0]
    commands = [
        {
            "command": primary.get("command") or "aipmc next",
            "reason": "Inspect the strongest existing match first.",
        }
    ]
    if primary.get("type") == "task":
        commands.append(
            {
                "command": f"aipmc task note --id {primary['id']} --content \"continue current work\"",
                "reason": "Use the existing task as the working thread if it already matches.",
            }
        )
    else:
        commands.append(
            {
                "command": "aipmc next",
                "reason": "Refresh the recommended mainline action after reviewing the match.",
            }
        )
    return commands
