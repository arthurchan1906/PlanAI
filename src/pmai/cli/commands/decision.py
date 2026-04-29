from __future__ import annotations

import argparse
from typing import Any, Dict, List

from ...store import (
    create_decision,
    get_decision,
    list_decisions,
    update_decision_status,
)
from .creation_guidance import (
    build_created_response,
    build_possible_duplicate_response,
    escape_cli_text,
    normalize_text,
    text_terms,
)
from .project import run_local_command


def _build_duplicate_payload(*, title: str, background: str, decision_text: str) -> Dict[str, Any] | None:
    normalized_title = normalize_text(title)
    title_terms = text_terms(title)
    background_terms = text_terms(background)
    decision_terms = text_terms(decision_text)
    if not normalized_title or not title_terms:
        return None

    escaped_title = escape_cli_text(title)
    candidates: List[Dict[str, Any]] = []
    for decision in list_decisions():
        existing_title = normalize_text(decision.get("title", ""))
        if not existing_title:
            continue

        score = 0
        reasons: List[str] = []
        if existing_title == normalized_title:
            score += 100
            reasons.append("exact_title_match")
        title_overlap = len(set(title_terms) & set(text_terms(decision.get("title", ""))))
        if title_overlap:
            score += title_overlap * 10
            reasons.append(f"title_term_overlap:{title_overlap}")
        if background_terms:
            background_overlap = len(set(background_terms) & set(text_terms(decision.get("background", ""))))
            if background_overlap:
                score += background_overlap * 4
                reasons.append(f"background_overlap:{background_overlap}")
        if decision_terms:
            decision_overlap = len(set(decision_terms) & set(text_terms(decision.get("decision", ""))))
            if decision_overlap:
                score += decision_overlap * 4
                reasons.append(f"decision_overlap:{decision_overlap}")

        if score < 10:
            continue
        candidates.append(
            {
                "type": "decision",
                "id": decision["id"],
                "title": decision["title"],
                "status": decision.get("status"),
                "date": decision.get("date"),
                "score": score,
                "reasons": reasons,
                "command": f"aipmc decision show --id {decision['id']}",
            }
        )

    if not candidates:
        return None

    candidates.sort(key=lambda item: (-item["score"], item["status"] != "proposed", item["title"]))
    primary = candidates[0]
    return build_possible_duplicate_response(
        message="Found similar existing decision(s). Reuse or inspect them before creating a new one.",
        input_payload={
            "title": title,
            "background": background,
            "decision": decision_text,
        },
        candidates=candidates,
        next_steps=[
            {
                "command": f"aipmc decision show --id {primary['id']}",
                "reason": "Inspect the closest existing decision first.",
            },
            {
                "command": f"aipmc decision review --id {primary['id']} --status accepted",
                "reason": "If this is the same decision and it is still pending, review the existing one instead of creating another.",
            },
            {
                "command": f"aipmc decision add --title \"{escaped_title}\" --background \"...\" --decision \"...\" --force-create",
                "reason": "Only create a new decision if these candidates do not fit.",
            },
        ],
    )


def _build_created_payload(decision: Dict[str, Any]) -> Dict[str, Any]:
    return build_created_response(
        kind="decision",
        payload=decision,
        next_steps=[
            {
                "command": f"aipmc decision show --id {decision['id']}",
                "reason": "Inspect the created decision and confirm the stored context.",
            },
            {
                "command": f"aipmc decision review --id {decision['id']} --status accepted",
                "reason": "Review it explicitly before downstream work depends on it.",
            },
        ],
    )


def handle_decision(args: argparse.Namespace) -> None:
    if args.decision_command == "list":
        run_local_command(lambda: {"decisions": list_decisions()})
    elif args.decision_command == "show":
        run_local_command(lambda: get_decision(args.id))
    elif args.decision_command == "add":
        def _add_decision() -> Dict[str, Any]:
            if not args.force_create:
                duplicate_payload = _build_duplicate_payload(
                    title=args.title,
                    background=args.background,
                    decision_text=args.decision,
                )
                if duplicate_payload is not None:
                    return duplicate_payload
            decision = create_decision(
                {
                    "title": args.title,
                    "background": args.background,
                    "decision": args.decision,
                    "status": args.status,
                }
            )
            return _build_created_payload(decision)

        run_local_command(
            _add_decision
        )
    elif args.decision_command == "review":
        run_local_command(lambda: update_decision_status(args.id, args.status))
