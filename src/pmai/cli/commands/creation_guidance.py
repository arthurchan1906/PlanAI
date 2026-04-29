from __future__ import annotations

import re
from typing import Any, Dict, List


def normalize_text(value: str) -> str:
    lowered = re.sub(r"[\W_]+", " ", (value or "").strip().lower())
    return " ".join(lowered.split())


def text_terms(value: str) -> List[str]:
    return [term for term in normalize_text(value).split() if term]


def escape_cli_text(value: str) -> str:
    return (value or "").replace('"', '\\"')


def build_possible_duplicate_response(
    *,
    message: str,
    input_payload: Dict[str, Any],
    candidates: List[Dict[str, Any]],
    next_steps: List[Dict[str, str]],
) -> Dict[str, Any]:
    return {
        "ok": False,
        "error": "possible_duplicate",
        "message": message,
        "input": input_payload,
        "candidates": candidates[:5],
        "next_steps": next_steps[:3],
    }


def build_created_response(
    *,
    kind: str,
    payload: Dict[str, Any],
    next_steps: List[Dict[str, str]],
) -> Dict[str, Any]:
    return {
        "ok": True,
        kind: payload,
        "next_steps": next_steps[:3],
    }
