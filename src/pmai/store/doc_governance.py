from __future__ import annotations

import os
import shutil
from pathlib import Path
from typing import Any, Dict, List

from .config import get_project_root
from .db import get_connection
from .docs import is_doc_path_normalized, list_doc_records, normalize_doc_path, resolve_doc_path, update_doc_record


SCAN_DOC_DIRS = ("doc", "docs")
STATUS_PRIORITY = {"active": 5, "draft": 4, "stale": 3, "obsolete": 2, "archived": 1}


def get_doc_dir(project_root: Path) -> Path:
    """???????????? doc/ ?????"""
    return project_root / "doc"


def get_archive_dir(doc_dir: Path) -> Path:
    """?????????? doc/archive/?"""
    return doc_dir / "archive"


def scan_physical_docs(project_root: Path) -> List[str]:
    """?????????????? project_root ???????"""
    md_files = set()

    for file in project_root.iterdir():
        if file.is_file() and file.suffix.lower() == ".md":
            md_files.add(normalize_doc_path(file.name))

    for directory_name in SCAN_DOC_DIRS:
        doc_dir = project_root / directory_name
        if not doc_dir.is_dir():
            continue
        archive_name = "archive" if directory_name == "doc" else None
        for root, dirs, files in os.walk(doc_dir):
            rel_root = Path(root).relative_to(project_root)
            if archive_name and archive_name in rel_root.parts:
                continue
            dirs[:] = [item for item in dirs if item != archive_name]
            for file in files:
                if not file.lower().endswith(".md"):
                    continue
                rel_path = rel_root / file
                md_files.add(normalize_doc_path(str(rel_path).replace("\\", "/")))
    return sorted(md_files)


def sync_docs_with_fs() -> Dict[str, Any]:
    """?????????????"""
    project_root = get_project_root()
    physical_files = set(scan_physical_docs(project_root))

    records = list_doc_records(normalize=False)
    db_paths = {normalize_doc_path(record["path"]) for record in records}

    untracked = sorted(physical_files - db_paths)
    for path in untracked:
        update_doc_record(
            {
                "path": path,
                "status": "draft",
                "type": "unknown",
                "layer": "exploration",
                "create": True,
            }
        )

    missing = sorted(db_paths - physical_files)

    return {
        "untracked_discovered": untracked,
        "missing_from_fs": missing,
        "total_synced": len(physical_files),
    }


def _pick_preferred_record(records: List[Dict[str, Any]]) -> Dict[str, Any]:
    def key(record: Dict[str, Any]):
        return (
            1 if is_doc_path_normalized(record["path"]) else 0,
            1 if record["source_of_truth"] else 0,
            STATUS_PRIORITY.get(record["status"], 0),
            record.get("last_reviewed") or "",
        )

    return max(records, key=key)


def repair_doc_records() -> Dict[str, Any]:
    """??????????????? doc links ???"""
    conn = get_connection()
    try:
        records = list_doc_records(normalize=False)
        grouped: Dict[str, List[Dict[str, Any]]] = {}
        for record in records:
            normalized = normalize_doc_path(record["path"])
            grouped.setdefault(normalized, []).append(record)

        renamed_paths = []
        normalized_superseded = []
        removed_duplicates = []
        conflict_groups = []
        updated_doc_links = []

        for normalized, items in grouped.items():
            preferred = _pick_preferred_record(items)
            other_items = [item for item in items if item["path"] != preferred["path"]]
            preferred_superseded = preferred.get("superseded_by")
            normalized_superseded_by = None
            if preferred_superseded:
                normalized_superseded_by = normalize_doc_path(preferred_superseded)

            if preferred["path"] != normalized:
                conn.execute("UPDATE doc_records SET path = ? WHERE path = ?", (normalized, preferred["path"]))
                conn.execute(
                    "UPDATE links SET source_id = ? WHERE source_type = 'doc' AND source_id = ?",
                    (normalized, preferred["path"]),
                )
                conn.execute(
                    "UPDATE links SET target_id = ? WHERE target_type = 'doc' AND target_id = ?",
                    (normalized, preferred["path"]),
                )
                renamed_paths.append({"from": preferred["path"], "to": normalized})
                if preferred["path"] != normalized:
                    updated_doc_links.append(preferred["path"])

            if preferred_superseded != normalized_superseded_by:
                conn.execute(
                    "UPDATE doc_records SET superseded_by = ? WHERE path = ?",
                    (normalized_superseded_by, normalized),
                )
                normalized_superseded.append({"path": normalized, "value": normalized_superseded_by})

            for item in other_items:
                item_superseded = item.get("superseded_by")
                normalized_item_superseded = normalize_doc_path(item_superseded) if item_superseded else None
                if item_superseded and normalized_item_superseded != item_superseded:
                    normalized_superseded.append({"path": item["path"], "value": normalized_item_superseded})

                if item["path"] == normalized:
                    conflict_groups.append({"normalized": normalized, "paths": [entry["path"] for entry in items]})
                    continue

                conn.execute(
                    "UPDATE links SET source_id = ? WHERE source_type = 'doc' AND source_id = ?",
                    (normalized, item["path"]),
                )
                conn.execute(
                    "UPDATE links SET target_id = ? WHERE target_type = 'doc' AND target_id = ?",
                    (normalized, item["path"]),
                )
                conn.execute(
                    "UPDATE doc_records SET superseded_by = ? WHERE superseded_by = ?",
                    (normalized, item["path"]),
                )
                conn.execute("DELETE FROM doc_records WHERE path = ?", (item["path"],))
                removed_duplicates.append({"from": item["path"], "to": normalized})
                updated_doc_links.append(item["path"])

        link_rows = conn.execute(
            "SELECT id, source_type, source_id, target_type, target_id FROM links WHERE source_type = 'doc' OR target_type = 'doc'"
        ).fetchall()
        for row in link_rows:
            if row["source_type"] == "doc":
                normalized_source = normalize_doc_path(row["source_id"])
                if normalized_source != row["source_id"]:
                    conn.execute("UPDATE links SET source_id = ? WHERE id = ?", (normalized_source, row["id"]))
                    updated_doc_links.append(row["id"])
            if row["target_type"] == "doc":
                normalized_target = normalize_doc_path(row["target_id"])
                if normalized_target != row["target_id"]:
                    conn.execute("UPDATE links SET target_id = ? WHERE id = ?", (normalized_target, row["id"]))
                    updated_doc_links.append(row["id"])

        superseded_rows = conn.execute("SELECT path, superseded_by FROM doc_records WHERE superseded_by IS NOT NULL").fetchall()
        for row in superseded_rows:
            normalized_value = normalize_doc_path(row["superseded_by"])
            if normalized_value != row["superseded_by"]:
                conn.execute("UPDATE doc_records SET superseded_by = ? WHERE path = ?", (normalized_value, row["path"]))
                normalized_superseded.append({"path": row["path"], "value": normalized_value})

        conn.commit()
        return {
            "renamed_paths": renamed_paths,
            "normalized_superseded_by": normalized_superseded,
            "removed_duplicates": removed_duplicates,
            "updated_doc_link_refs": sorted(set(updated_doc_links)),
            "conflict_groups": conflict_groups,
        }
    finally:
        conn.close()


def prune_archived_docs() -> Dict[str, Any]:
    """??????? archived ?????????"""
    project_root = get_project_root()
    doc_dir = get_doc_dir(project_root)
    archive_dir = get_archive_dir(doc_dir)

    records = list_doc_records(status="archived")
    moved = []
    errors = []

    if records and not archive_dir.exists():
        archive_dir.mkdir(parents=True, exist_ok=True)

    for record in records:
        path = normalize_doc_path(record["path"])
        try:
            src = resolve_doc_path(project_root, path)
        except ValueError as exc:
            errors.append({"path": path, "error": str(exc)})
            continue
        if not src.exists():
            continue

        dst = archive_dir / Path(path).name
        if dst.exists():
            timestamp = int(os.path.getmtime(src))
            dst = archive_dir / f"{timestamp}_{Path(path).name}"

        try:
            shutil.move(str(src), str(dst))
            moved.append(path)
        except Exception as exc:
            errors.append({"path": path, "error": str(exc)})

    return {
        "moved": moved,
        "errors": errors,
        "archive_path": str(archive_dir.relative_to(project_root)).replace("\\", "/") if moved else None,
    }


def audit_governance() -> Dict[str, Any]:
    """???????"""
    records = list_doc_records()

    layer_sot: Dict[str, List[str]] = {}
    for record in records:
        if record["source_of_truth"]:
            layer_sot.setdefault(record["layer"], []).append(record["path"])

    conflicts = {layer: paths for layer, paths in layer_sot.items() if len(paths) > 1}
    stale_active = [record["path"] for record in records if record["status"] == "active" and record["superseded_by"]]

    return {
        "sot_conflicts": conflicts,
        "stale_active_records": stale_active,
    }


def audit_docs_comprehensive() -> Dict[str, Any]:
    """????????????????????????"""
    project_root = get_project_root()
    basic_records = list_doc_records(normalize=False)
    physical_files = set(scan_physical_docs(project_root))
    gov_audit = audit_governance()

    obsolete_without_replacement = [
        row["path"] for row in basic_records if row["status"] == "obsolete" and not row["superseded_by"]
    ]
    invalid_truth_records = [
        row["path"]
        for row in basic_records
        if row["status"] in {"archived", "obsolete"} and row["source_of_truth"]
    ]
    tracked_paths = {row["path"] for row in basic_records}
    missing_from_fs = sorted(row["path"] for row in basic_records if normalize_doc_path(row["path"]) not in physical_files)
    untracked_in_fs = sorted(path for path in physical_files if path not in {normalize_doc_path(item) for item in tracked_paths})
    path_not_normalized = sorted(row["path"] for row in basic_records if not is_doc_path_normalized(row["path"]))

    active_records = [row for row in basic_records if row["status"] == "active"]
    source_of_truth_records = [row for row in basic_records if row["source_of_truth"]]

    return {
        "total_managed_docs": len(basic_records),
        "tracked_files_in_fs": len(physical_files),
        "active_records": len(active_records),
        "source_of_truth_records": len(source_of_truth_records),
        "sot_conflicts": gov_audit["sot_conflicts"],
        "stale_active_records": gov_audit["stale_active_records"],
        "obsolete_without_replacement": obsolete_without_replacement,
        "invalid_truth_records": invalid_truth_records,
        "missing_from_fs": missing_from_fs,
        "untracked_in_fs": untracked_in_fs,
        "path_not_normalized": path_not_normalized,
    }
