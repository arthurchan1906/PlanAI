from __future__ import annotations

import subprocess
from typing import Any, Dict, List, Optional

from .config import get_project_root


def _run_git(args: List[str]) -> Optional[str]:
    try:
        completed = subprocess.run(
            ["git", "-c", "core.quotepath=false", *args],
            cwd=str(get_project_root()),
            capture_output=True,
            text=True,
            check=False,
            encoding="utf-8",
        )
    except OSError:
        return None
    if completed.returncode != 0:
        return None
    return completed.stdout.rstrip()


def _normalize_git_path(value: str) -> str:
    candidate = value.strip().replace('\\', '/')
    if candidate.startswith('"') and candidate.endswith('"'):
        candidate = candidate[1:-1]
    if ' -> ' in candidate:
        candidate = candidate.split(' -> ', 1)[1].strip()
    return candidate


def _normalize_file_list(values: List[str]) -> List[str]:
    files: List[str] = []
    for value in values:
        candidate = _normalize_git_path(value)
        if not candidate:
            continue
        if candidate not in files:
            files.append(candidate)
    return files


def get_git_worktree_status() -> Dict[str, Any]:
    branch = _run_git(["rev-parse", "--abbrev-ref", "HEAD"]) or ""
    output = _run_git(["status", "--short"]) or ""
    files: List[Dict[str, Any]] = []
    staged: List[str] = []
    unstaged: List[str] = []
    untracked: List[str] = []

    for line in output.splitlines():
        if len(line) < 3:
            continue
        index_status = line[0]
        worktree_status = line[1]
        path = _normalize_git_path(line[3:])
        if not path or path.startswith('.pmai/'):
            continue
        item = {
            "path": path,
            "index_status": index_status,
            "worktree_status": worktree_status,
        }
        files.append(item)
        if index_status not in (' ', '?') and path not in staged:
            staged.append(path)
        if worktree_status not in (' ', '?') and path not in unstaged:
            unstaged.append(path)
        if index_status == '?' and worktree_status == '?' and path not in untracked:
            untracked.append(path)

    return {
        "branch": branch,
        "dirty": bool(files),
        "changed_files_count": len(files),
        "staged": staged,
        "unstaged": unstaged,
        "untracked": untracked,
        "files": files,
    }


def get_git_diff(*, staged: bool = False) -> Dict[str, Any]:
    return get_git_diff_summary(staged=staged)


def get_git_diff_summary(*, staged: bool = False) -> Dict[str, Any]:
    branch = _run_git(["rev-parse", "--abbrev-ref", "HEAD"]) or ""
    worktree = get_git_worktree_status()
    diff_args = ["diff", "--stat", "--compact-summary"]
    name_only_args = ["diff", "--name-only", "--relative"]
    if staged:
        diff_args.append("--cached")
        name_only_args.append("--cached")
    diff_output = _run_git(diff_args) or ""
    names_output = _run_git(name_only_args) or ""
    files = _normalize_file_list(names_output.splitlines())
    stat_lines = [line.rstrip() for line in diff_output.splitlines() if line.strip()]
    return {
        "branch": branch,
        "scope": "staged" if staged else "worktree",
        "dirty": worktree.get("dirty", False),
        "files": files,
        "stats": stat_lines,
        "has_diff": bool(files or stat_lines),
    }


def get_git_recent_commits(limit: int = 10) -> List[Dict[str, Any]]:
    return list_recent_git_commits(limit)


def list_recent_git_commits(limit: int = 5) -> List[Dict[str, Any]]:
    if limit <= 0:
        return []
    output = _run_git([
        "log",
        f"-n{limit}",
        "--date=iso-strict",
        "--name-only",
        "--pretty=format:%H%x1f%an%x1f%ad%x1f%s",
    ])
    if not output:
        return []

    commits: List[Dict[str, Any]] = []
    current: Optional[Dict[str, Any]] = None
    for raw_line in output.splitlines():
        line = raw_line.rstrip()
        if not line:
            continue
        if "\x1f" in line:
            if current is not None:
                commits.append(current)
            meta = line.split("\x1f")
            if len(meta) != 4:
                current = None
                continue
            current = {
                "commit_hash": meta[0],
                "author": meta[1],
                "timestamp": meta[2],
                "title": meta[3],
                "files": [],
            }
            continue
        if current is None:
            continue
        file_path = _normalize_git_path(line)
        if not file_path or file_path.startswith('.pmai/'):
            continue
        if file_path not in current["files"]:
            current["files"].append(file_path)

    if current is not None:
        commits.append(current)
    return commits


def _get_git_commit_snapshot(commit_hash: str) -> Dict[str, Any]:
    if not commit_hash:
        return {
            "found": False,
            "commit_hash": "",
            "title": "",
            "author": "",
            "timestamp": "",
            "files": [],
            "stats": [],
        }

    meta = _run_git(["show", "-s", "--date=iso-strict", "--format=%H%x1f%an%x1f%ad%x1f%s", commit_hash])
    if not meta:
        return {
            "found": False,
            "commit_hash": commit_hash,
            "title": "",
            "author": "",
            "timestamp": "",
            "files": [],
            "stats": [],
        }

    meta_parts = meta.split("\x1f")
    files_output = _run_git(["show", "--pretty=format:", "--name-only", commit_hash]) or ""
    stats_output = _run_git(["show", "--stat", "--oneline", "--format=%h %s", commit_hash]) or ""
    files: List[str] = []
    for line in files_output.splitlines():
        file_path = _normalize_git_path(line)
        if not file_path or file_path.startswith('.pmai/'):
            continue
        if file_path not in files:
            files.append(file_path)
    stats = [line.rstrip() for line in stats_output.splitlines() if line.strip()]
    return {
        "found": True,
        "commit_hash": meta_parts[0] if len(meta_parts) > 0 else commit_hash,
        "author": meta_parts[1] if len(meta_parts) > 1 else "",
        "timestamp": meta_parts[2] if len(meta_parts) > 2 else "",
        "title": meta_parts[3] if len(meta_parts) > 3 else "",
        "files": files,
        "stats": stats,
    }


def infer_git_metadata() -> Dict[str, Any]:
    branch = _run_git(["rev-parse", "--abbrev-ref", "HEAD"]) or ""
    commit_hash = _run_git(["rev-parse", "HEAD"]) or ""

    files: List[str] = []
    for args in [
        ["diff", "--name-only", "--relative"],
        ["diff", "--name-only", "--relative", "--cached"],
        ["ls-files", "--others", "--exclude-standard"],
    ]:
        output = _run_git(args)
        if not output:
            continue
        for line in output.splitlines():
            candidate = line.strip().replace("\\", "/")
            if candidate.startswith('"') and candidate.endswith('"'):
                candidate = candidate[1:-1]
            if candidate.startswith(".pmai/"):
                continue
            if candidate and candidate not in files:
                files.append(candidate)

    return {
        "branch": branch,
        "commit_hash": commit_hash,
        "files": files,
    }
