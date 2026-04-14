from __future__ import annotations

from typing import Any, Dict

from .db import get_connection
from .decisions import create_decision
from .links import create_link, list_links
from .tasks import create_task


def convert_idea(idea_id: str, target_type: str) -> Dict[str, Any]:
    from .ideas import get_idea, update_idea

    idea = get_idea(idea_id)
    target_type = target_type.strip().lower()
    if target_type not in {"task", "decision"}:
        raise ValueError(f"unsupported idea conversion target: {target_type}")

    existing = get_idea_conversion(idea_id)
    if existing:
        return {
            "idea": get_idea(idea_id),
            "converted": existing,
            "already_converted": True,
        }

    if target_type == "task":
        target = create_task(
            {
                "title": idea["title"],
                "priority": "P1",
                "status": "todo",
                "phase": "general",
                "acceptance": _idea_acceptance(idea),
            }
        )
        target_id = target["id"]
        create_link(
            {
                "source_type": "idea",
                "source_id": idea_id,
                "relation": "converted_to",
                "target_type": "task",
                "target_id": target_id,
                "note": "Converted from idea thread",
            }
        )
    else:
        target = create_decision(
            {
                "title": idea["title"],
                "background": _idea_decision_background(idea),
                "decision": _idea_decision_draft(idea),
                "status": "proposed",
            }
        )
        target_id = target["id"]
        create_link(
            {
                "source_type": "idea",
                "source_id": idea_id,
                "relation": "converted_to",
                "target_type": "decision",
                "target_id": target_id,
                "note": "Converted from idea thread",
            }
        )

    update_idea(
        idea_id,
        {
            "status": "accepted" if target_type == "decision" else "under_review",
            "recommended_next_action": f"converted_to_{target_type}",
        },
    )
    return {
        "idea": get_idea(idea_id),
        "converted": {
            "type": target_type,
            "id": target_id,
            "title": target["title"],
        },
        "already_converted": False,
    }


def get_idea_conversion(idea_id: str) -> Dict[str, Any] | None:
    outgoing = list_links(source_id=idea_id, relation="converted_to")
    if not outgoing:
        return None
    link = outgoing[0]
    title = _resolve_target_title(link["target_type"], link["target_id"])
    return {
        "type": link["target_type"],
        "id": link["target_id"],
        "title": title,
    }


def enrich_idea_with_conversion(idea: Dict[str, Any]) -> Dict[str, Any]:
    converted = get_idea_conversion(idea["id"])
    if not converted:
        return {**idea, "converted_to": None}
    return {
        **idea,
        "converted_to": converted["id"],
        "converted_to_type": converted["type"],
        "converted_to_title": converted.get("title", ""),
    }


def _idea_acceptance(idea: Dict[str, Any]) -> list[str]:
    items = []
    if idea.get("current_summary"):
        items.append(f"Deliver the idea outcome described by: {idea['current_summary']}")
    if idea.get("main_question"):
        items.append(f"Resolve the key question: {idea['main_question']}")
    if not items:
        items.append(f"Implement the outcome implied by idea `{idea['title']}`.")
    return items


def _idea_decision_background(idea: Dict[str, Any]) -> str:
    parts = []
    if idea.get("current_summary"):
        parts.append(f"Current summary: {idea['current_summary']}")
    elif idea.get("summary"):
        parts.append(f"Initial idea: {idea['summary']}")
    if idea.get("main_question"):
        parts.append(f"Open question: {idea['main_question']}")
    if idea.get("impact"):
        parts.append(f"Impact scope: {idea['impact']}")
    return "\n".join(parts) if parts else idea["title"]


def _idea_decision_draft(idea: Dict[str, Any]) -> str:
    if idea.get("main_question"):
        return (
            f"Decide how to resolve `{idea['main_question']}` based on the current idea thread.\n"
            f"Proposed direction: {idea.get('current_summary') or idea.get('summary') or idea['title']}"
        )
    return f"Adopt the idea direction: {idea.get('current_summary') or idea.get('summary') or idea['title']}"


def _resolve_target_title(target_type: str, target_id: str) -> str:
    table_name = "tasks" if target_type == "task" else "decisions" if target_type == "decision" else ""
    if not table_name:
        return ""
    conn = get_connection()
    try:
        row = conn.execute(f"SELECT title FROM {table_name} WHERE id = ?", (target_id,)).fetchone()
        return row["title"] if row else ""
    finally:
        conn.close()
