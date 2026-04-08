from __future__ import annotations

import json
import os
from typing import Any
from urllib import error, request


DEFAULT_FEEDBACK_BASE_URL = "http://43.167.206.218:8080"
DEFAULT_FEEDBACK_TIMEOUT_SECONDS = 8


def get_feedback_base_url(override: str | None = None) -> str:
    return (override or os.getenv("PMAI_FEEDBACK_BASE_URL") or DEFAULT_FEEDBACK_BASE_URL).rstrip("/")


def _request_json(url: str, method: str, payload: dict[str, Any] | None = None) -> Any:
    data = None
    headers = {"Accept": "application/json"}
    if payload is not None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = request.Request(url, data=data, headers=headers, method=method)
    try:
        with request.urlopen(req, timeout=DEFAULT_FEEDBACK_TIMEOUT_SECONDS) as response:
            body = response.read().decode("utf-8")
    except error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"Feedback API HTTP {exc.code}: {detail}") from exc
    except error.URLError as exc:
        raise RuntimeError(f"Feedback API request failed: {exc.reason}") from exc

    if not body.strip():
        return {"ok": True}
    try:
        return json.loads(body)
    except json.JSONDecodeError:
        return {"raw": body}


def list_feedback(base_url: str | None = None) -> Any:
    return _request_json(f"{get_feedback_base_url(base_url)}/pmai/list", "GET")


def add_feedback(label: str, content: str, base_url: str | None = None) -> Any:
    return _request_json(
        f"{get_feedback_base_url(base_url)}/pmai/add",
        "POST",
        {"label": label, "content": content},
    )
