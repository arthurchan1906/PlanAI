from __future__ import annotations

import argparse
import inspect
import json
import os
import platform
import shutil
import sys
from typing import Any

from ...bootstrap import bootstrap_project_db
from ...agent_guide import write_agent_guide
from ...feedback_api import get_feedback_base_url
from ...store import (
    describe_runtime,
    get_config_path,
    get_db_path,
    get_runtime_dir,
    load_runtime_config,
    save_runtime_config,
)
from ...usage_guide import build_usage_markdown, write_usage_file


def init_project(args: argparse.Namespace) -> None:
    get_runtime_dir(create=True)
    if args.port is not None or args.host:
        payload: dict[str, Any] = {}
        if args.host:
            payload["web_host"] = args.host
        if args.port is not None:
            payload["web_port"] = args.port
        save_runtime_config(payload)
    elif not get_config_path().exists():
        save_runtime_config({})
    bootstrap_project_db()
    write_usage_file()
    agent_guide = {"written": [], "skipped": []}
    if args.write_agent_guide or args.force_agent_guide:
        agent_guide = write_agent_guide(force=args.force_agent_guide)
    print(
        json.dumps(
            {
                "ok": True,
                "message": "Initialized .pmai",
                "agent_guide": agent_guide,
                "agent_instructions_command": "aipmc agent instructions",
            },
            ensure_ascii=False,
            indent=2,
        )
    )


def show_info() -> None:
    print(json.dumps(describe_runtime(), ensure_ascii=False, indent=2))


def show_help_text() -> None:
    print(build_usage_markdown())


def show_doctor() -> None:
    runtime = describe_runtime()
    config = load_runtime_config()
    db_path = get_db_path()
    runtime_dir = get_runtime_dir()
    print(
        json.dumps(
            {
                "python_executable": sys.executable,
                "python_version": sys.version.split()[0],
                "platform": platform.platform(),
                "cwd": os.getcwd(),
                "command_resolution": {
                    "aipmc": shutil.which("aipmc"),
                    "aipmv": shutil.which("aipmv"),
                    "python": shutil.which("python"),
                },
                "runtime": runtime,
                "config": config,
                "feedback_base_url": get_feedback_base_url(),
                "filesystem": {
                    "runtime_dir_exists": runtime_dir.exists(),
                    "runtime_dir_writable": runtime_dir.exists() and os.access(runtime_dir, os.W_OK),
                    "db_parent_exists": db_path.parent.exists(),
                    "db_parent_writable": db_path.parent.exists() and os.access(db_path.parent, os.W_OK),
                },
                "hints": [
                    "Use pipx for CLI-style installation if commands are not stable across Python environments.",
                    "If aipmc is not found, fallback to `python -m pmai ...`.",
                    "If runtime_dir_writable is false, writes to .pmai may fail in container or sandbox environments.",
                ],
            },
            ensure_ascii=False,
            indent=2,
        )
    )


def run_remote_command(fn) -> None:
    try:
        print(json.dumps(fn(), ensure_ascii=False, indent=2))
    except Exception as exc:
        print(
            json.dumps(
                {
                    "ok": False,
                    "detail": str(exc),
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None


def _infer_meta() -> dict[str, Any] | None:
    for frame_info in inspect.stack():
        func_name = frame_info.function
        args = frame_info.frame.f_locals.get("args")
        if args is None:
            continue
        if func_name.startswith("handle_"):
            entity = func_name[len("handle_"):]
            command_attr = f"{entity}_command"
            action = getattr(args, command_attr, None)
            if action is None:
                continue
            return {
                "command": f"{entity}.{action}",
                "entity_type": entity,
                "details": {k: str(v) for k, v in vars(args).items() if not k.endswith("_command")},
            }
        if func_name == "main":
            entity = getattr(args, "command", None)
            if entity is None:
                continue
            sub_attr = f"{entity}_command"
            action = getattr(args, sub_attr, None)
            if action is None:
                action = "execute"
            return {
                "command": f"{entity}.{action}",
                "entity_type": entity,
                "details": {k: str(v) for k, v in vars(args).items() if k not in ("command", sub_attr)},
            }
    return None


def _get_log_path():
    from ...store import get_runtime_dir
    return get_runtime_dir() / "operation.log"


def _record_log(result: Any, success: bool) -> None:
    try:
        meta = _infer_meta()
        if meta is None:
            return

        from datetime import datetime

        entity_id = None
        if isinstance(result, dict):
            entity_id = result.get("id")

        entry = {
            "timestamp": datetime.now().isoformat(timespec="seconds"),
            "command": meta["command"],
            "entity_type": meta["entity_type"],
            "entity_id": entity_id,
            "success": success,
            "args": meta.get("details", {}),
        }

        log_path = _get_log_path()
        log_path.parent.mkdir(parents=True, exist_ok=True)
        with open(log_path, "a", encoding="utf-8") as f:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")
    except Exception:
        pass


def run_local_command(fn, *, init_hint: str = "Run `aipmc init` in the project root first.") -> None:
    try:
        result = fn()
        print(json.dumps(result, ensure_ascii=False, indent=2))
        _record_log(result, success=True)
    except FileNotFoundError as exc:
        _record_log(None, success=False)
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": "runtime_not_initialized",
                    "detail": str(exc),
                    "hint": init_hint,
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None
    except KeyError as exc:
        _record_log(None, success=False)
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": "not_found",
                    "detail": str(exc),
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None
    except Exception as exc:
        _record_log(None, success=False)
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": "command_failed",
                    "detail": str(exc),
                },
                ensure_ascii=False,
                indent=2,
            ),
            file=sys.stderr,
        )
        raise SystemExit(2) from None
