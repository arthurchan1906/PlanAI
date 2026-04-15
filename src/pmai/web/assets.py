from __future__ import annotations

import os
from pathlib import Path


def resolve_web_dir() -> Path:
    # 强制优先使用 package 内部的 dist，这是发布后的正式位置
    # parents[1] 是 src/pmai
    package_dist = Path(__file__).resolve().parents[1] / "ui" / "dist"
    
    # 只有在开发环境下（比如 PlanAI 根目录下）才考虑外部的 ui/dist
    # parents[3] 是 PlanAI
    repo_dist = Path(__file__).resolve().parents[3] / "ui" / "dist"

    # 如果在 src 目录下运行，优先使用同步后的包内 dist
    if package_dist.exists() and package_dist.is_dir():
        return package_dist
        
    if repo_dist.exists():
        return repo_dist
        
    return package_dist
