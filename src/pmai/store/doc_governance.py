from __future__ import annotations

import os
import shutil
from pathlib import Path
from typing import Any, Dict, List, Set, Optional

from .config import get_project_root
from .docs import list_doc_records, update_doc_record


def get_doc_dir(project_root: Path) -> Path:
    """获取文档根目录，默认为 doc/"""
    return project_root / "doc"


def get_archive_dir(doc_dir: Path) -> Path:
    """获取归档目录，默认为 doc/archive/"""
    return doc_dir / "archive"


def scan_physical_docs(project_root: Path) -> List[str]:
    """扫描项目根目录下的特定文档位置，返回相对于 project_root 的路径"""
    # 策略：扫描根目录下的 .md 文件，以及 doc/ 目录下的 .md 文件
    md_files = []
    
    # 1. 扫描根目录
    for file in os.listdir(project_root):
        if file.endswith(".md"):
            md_files.append(file)
            
    # 2. 扫描 doc/ 目录
    doc_dir = get_doc_dir(project_root)
    if doc_dir.is_dir():
        archive_name = "archive"
        for root, dirs, files in os.walk(doc_dir):
            rel_root = Path(root).relative_to(project_root)
            # 排除 archive 目录
            if archive_name in rel_root.parts:
                continue
                
            for file in files:
                if file.endswith(".md"):
                    rel_path = rel_root / file
                    # 规范化路径分隔符为正斜杠
                    md_files.append(str(rel_path).replace("\\", "/"))
    return md_files


def sync_docs_with_fs() -> Dict[str, Any]:
    """同步数据库记录与物理文件"""
    project_root = get_project_root()
    physical_files = set(scan_physical_docs(project_root))
    
    records = list_doc_records()
    db_paths = {r["path"].replace("\\", "/") for r in records}
    
    # 1. 发现未跟踪的文件 (Untracked)
    untracked = physical_files - db_paths
    for path in untracked:
        # 自动注册为 draft
        update_doc_record({
            "path": path,
            "status": "draft",
            "type": "unknown",
            "layer": "exploration",
            "create": True
        })
        
    # 2. 发现数据库中有记录但物理上丢失的文件 (Missing)
    missing = db_paths - physical_files
    
    return {
        "untracked_discovered": list(untracked),
        "missing_from_fs": list(missing),
        "total_synced": len(physical_files)
    }


def prune_archived_docs() -> Dict[str, Any]:
    """物理移动标记为 archived 的文件到归档目录"""
    project_root = get_project_root()
    doc_dir = get_doc_dir(project_root)
    archive_dir = get_archive_dir(doc_dir)
    
    records = list_doc_records(status="archived")
    moved = []
    errors = []
    
    if records and not archive_dir.exists():
        archive_dir.mkdir(parents=True, exist_ok=True)
        
    for record in records:
        path = record["path"]
        src = project_root / path
        if not src.exists():
            continue
            
        dst = archive_dir / Path(path).name
        # 如果目标已存在，增加时间戳前缀防止覆盖
        if dst.exists():
            timestamp = os.path.getmtime(src)
            dst = archive_dir / f"{int(timestamp)}_{Path(path).name}"
            
        try:
            shutil.move(str(src), str(dst))
            moved.append(path)
        except Exception as e:
            errors.append({"path": path, "error": str(e)})
            
    return {
        "moved": moved,
        "errors": errors,
        "archive_path": str(archive_dir.relative_to(project_root)) if moved else None
    }


def audit_governance() -> Dict[str, Any]:
    """高级治理审计"""
    records = list_doc_records()
    
    # 冲突检查：同一个 layer 只能有一个 source_of_truth
    layer_sot: Dict[str, List[str]] = {}
    for r in records:
        if r["source_of_truth"]:
            layer = r["layer"]
            if layer not in layer_sot:
                layer_sot[layer] = []
            layer_sot[layer].append(r["path"])
            
    conflicts = {layer: paths for layer, paths in layer_sot.items() if len(paths) > 1}
    
    # 陈旧检查：status='active' 但 superseded_by 有值的
    stale_active = [
        r["path"] for r in records if r["status"] == "active" and r["superseded_by"]
    ]
    
    return {
        "sot_conflicts": conflicts,
        "stale_active_records": stale_active,
    }


def audit_docs_comprehensive() -> Dict[str, Any]:
    """综合文档审计，结合基础检查与治理规则"""
    basic_records = list_doc_records()
    gov_audit = audit_governance()
    
    # 合并原有 audit_docs 的逻辑
    obsolete_without_replacement = [
        row["path"] for row in basic_records if row["status"] == "obsolete" and not row["superseded_by"]
    ]
    invalid_truth_records = [
        row["path"]
        for row in basic_records
        if row["status"] in {"archived", "obsolete"} and row["source_of_truth"]
    ]
    
    active_records = [row for row in basic_records if row["status"] == "active"]
    source_of_truth_records = [row for row in basic_records if row["source_of_truth"]]
    
    return {
        "total_managed_docs": len(basic_records),
        "active_records": len(active_records),
        "source_of_truth_records": len(source_of_truth_records),
        "sot_conflicts": gov_audit["sot_conflicts"],
        "stale_active_records": gov_audit["stale_active_records"],
        "obsolete_without_replacement": obsolete_without_replacement,
        "invalid_truth_records": invalid_truth_records,
    }
