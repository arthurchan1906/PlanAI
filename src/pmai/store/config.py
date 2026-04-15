from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any, Dict, Optional

RUNTIME_DIRNAME = ".pmai"
DB_FILENAME = "pmai.db"
CONFIG_FILENAME = "config.json"
DEFAULT_WEB_HOST = "127.0.0.1"
DEFAULT_WEB_PORT = 8011


def find_runtime_dir(start: Optional[Path] = None) -> Optional[Path]:
    override = os.getenv("PMAI_HOME") or os.getenv("PLANAI_HOME") or os.getenv("PROJECT_OS_HOME")
    if override:
        return Path(override).expanduser().resolve()

    current = (start or Path.cwd()).resolve()
    for candidate_root in [current, *current.parents]:
        runtime_dir = candidate_root / RUNTIME_DIRNAME
        if runtime_dir.is_dir():
            return runtime_dir
    return None


def get_project_root(start: Optional[Path] = None) -> Path:
    # 增加环境变量优先级，方便开发调试
    override = os.getenv("PMAI_PROJECT_ROOT")
    if override:
        return Path(override).expanduser().resolve()
        
    runtime_dir = find_runtime_dir(start)
    if runtime_dir:
        return runtime_dir.parent
    return (start or Path.cwd()).resolve()


def get_runtime_dir(start: Optional[Path] = None, create: bool = False) -> Path:
    runtime_dir = find_runtime_dir(start)
    if runtime_dir is None:
        runtime_dir = get_project_root(start) / RUNTIME_DIRNAME
    if create:
        runtime_dir.mkdir(parents=True, exist_ok=True)
    return runtime_dir


def get_db_path(start: Optional[Path] = None, create_parent: bool = False) -> Path:
    runtime_dir = get_runtime_dir(start, create=create_parent)
    data_dir = runtime_dir / "data"
    if create_parent:
        data_dir.mkdir(parents=True, exist_ok=True)
    return data_dir / DB_FILENAME


def get_journal_path(start: Optional[Path] = None) -> Path:
    db_path = get_db_path(start)
    return db_path.parent / f"{DB_FILENAME}-journal"


def get_config_path(start: Optional[Path] = None, create_parent: bool = False) -> Path:
    return get_runtime_dir(start, create=create_parent) / CONFIG_FILENAME


def load_runtime_config(start: Optional[Path] = None) -> Dict[str, Any]:
    config = {"web_host": DEFAULT_WEB_HOST, "web_port": DEFAULT_WEB_PORT}
    config_path = get_config_path(start)
    if not config_path.exists():
        return config
    try:
        loaded = json.loads(config_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return config
    if isinstance(loaded, dict):
        config.update(loaded)
    return config


def write_runtime_config(payload: Dict[str, Any], start: Optional[Path] = None) -> Dict[str, Any]:
    config = load_runtime_config(start)
    config.update(payload)
    config_path = get_config_path(start, create_parent=True)
    config_path.write_text(json.dumps(config, ensure_ascii=False, indent=2), encoding="utf-8")
    return config


def save_runtime_config(payload: Dict[str, Any], start: Optional[Path] = None) -> Dict[str, Any]:
    return write_runtime_config(payload, start)


def describe_runtime(start: Optional[Path] = None) -> Dict[str, str]:
    return {
        "project_root": str(get_project_root(start)),
        "runtime_dir": str(get_runtime_dir(start)),
        "database": str(get_db_path(start)),
        "config": str(get_config_path(start)),
    }
