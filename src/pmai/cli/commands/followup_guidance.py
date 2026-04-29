from __future__ import annotations

from typing import Any, Dict, List


def build_guided_response(
    *,
    ok: bool = True,
    message: str = "",
    payload: Dict[str, Any] | None = None,
    context_updates: List[Dict[str, Any]] | None = None,
    next_steps: List[Dict[str, Any]] | None = None,
) -> Dict[str, Any]:
    result: Dict[str, Any] = {"ok": ok}
    if message:
        result["message"] = message
    if payload:
        result.update(payload)
    if context_updates:
        result["context_updates"] = context_updates
    if next_steps:
        result["next_steps"] = next_steps
    return result
