from __future__ import annotations

from pathlib import Path

try:
    from .store import get_runtime_dir
except ImportError:
    from store import get_runtime_dir


USAGE_FILENAME = "USAGE.md"


def build_usage_markdown() -> str:
    return """# PMAI Usage

PMAI 是给 AI 编码项目使用的本地项目管理工具。

建议 AI 先读：
- `.pmai/USAGE.md`
- `.pmai/data/pmai.json`
- `README.md`

常用命令：

```bash
pmai help
pmai info
pmai canon show
pmai task list
pmai decision list
pmai commit list
pmai idea list
pmai daily show
pmai-web
```

初始化：

```bash
pmai init
```

这会创建：
- `.pmai/config.json`
- `.pmai/data/pmai.json`
- `.pmai/USAGE.md`

常用写入命令：

```bash
pmai task add --title "实现 xxx" --acceptance "完成 yyy"
pmai task update --id <task-id> --status in_progress
pmai decision add --title "采用方案A" --background "..." --decision "..."
pmai commit add --title "实现任务" --summary "..." --files a.py b.py
pmai idea capture --title "优化想法" --summary "..."
pmai daily close --completed "..." --problems "..." --risks "..." --next "..."
```

状态查看：

```bash
pmai info
pmai canon show
pmai task list
pmai commit list
pmai decision list
pmai idea list
pmai daily show
```

Web：

```bash
pmai-web
```

默认地址：
- `http://127.0.0.1:8011/`

说明：
- 这是单人本地工具，运行数据保存在项目内 `.pmai/`
- 从 PyPI 安装后直接使用 `pmai` 和 `pmai-web`
- Windows 下 `pip install` 后会自动生成对应命令入口，无需手写 `.cmd` 或额外可执行文件
"""


def get_usage_path(start: Path | None = None) -> Path:
    return get_runtime_dir(start, create=True) / USAGE_FILENAME


def write_usage_file(start: Path | None = None) -> Path:
    path = get_usage_path(start)
    path.write_text(build_usage_markdown(), encoding="utf-8")
    return path
