from __future__ import annotations

from pathlib import Path

try:
    from .store import get_runtime_dir
except ImportError:
    from store import get_runtime_dir


USAGE_FILENAME = "USAGE.md"


def build_usage_markdown() -> str:
    return """# PlanAI Usage

PlanAI 是给 AI 编码使用的本地项目管理工具。

建议 AI 先读：
- `.planai/USAGE.md`
- `.planai/data/planai.json`
- `README.md`

常用命令：

```bash
planai help
planai info
planai canon show
planai task list
planai decision list
planai commit list
planai idea list
planai daily show
planai-web
```

初始化：

```bash
planai init
```

这会创建：
- `.planai/config.json`
- `.planai/data/planai.json`
- `.planai/USAGE.md`

常用写入命令：

```bash
planai task add --title "实现 xxx" --acceptance "完成 yyy"
planai task update --id <task-id> --status in_progress
planai decision add --title "采用方案A" --background "..." --decision "..."
planai commit add --title "实现任务" --summary "..." --files a.py b.py
planai idea capture --title "优化想法" --summary "..."
planai daily close --completed "..." --problems "..." --risks "..." --next "..."
```

状态查看：

```bash
planai info
planai canon show
planai task list
planai commit list
planai decision list
planai idea list
planai daily show
```

Web：

```bash
planai-web
```

默认地址：
- `http://127.0.0.1:8011/`

说明：
- 这是单人本地工具，运行数据保存在项目内 `.planai/`
- 安装后直接使用 `planai` 和 `planai-web`
- Windows 下 `pip install` 后会自动生成 `planai.exe` / `planai-web.exe`，不需要手写 `.cmd` 或额外可执行文件
"""


def get_usage_path(start: Path | None = None) -> Path:
    return get_runtime_dir(start, create=True) / USAGE_FILENAME


def write_usage_file(start: Path | None = None) -> Path:
    path = get_usage_path(start)
    path.write_text(build_usage_markdown(), encoding="utf-8")
    return path
