try:
    from .json_store import bootstrap_state
    from .store import today
except ImportError:
    from json_store import bootstrap_state
    from store import today


def bootstrap_project_db() -> bool:
    bootstrap_state(
        {
            "meta": {
                "project_name": "pmai-project",
                "project_type": "ai-coding-project-manager",
                "runtime_root": ".pmai",
                "runtime_database": ".pmai/data/pmai.json",
                "runtime_config": ".pmai/config.json",
                "ai_read_order": [
                    ".pmai/data/pmai.json:canon",
                    ".pmai/data/pmai.json:tasks",
                    ".pmai/data/pmai.json:doc_records",
                    "README.md",
                    "pyproject.toml",
                ],
            },
            "canon": {
                "id": "canon-current",
                "updated_at": today(),
                "product_goal": "为 AI 编码项目提供可安装的项目管理工具，支持 CLI 记录、Web 查看和项目级初始化。",
                "engineering_focus": "统一隐藏目录、CLI、JSON 数据层和内置 Web 服务，确保复制目录后仍能正常运行。",
                "architecture": "Python stdlib CLI + HTTPServer + JSON + React 静态页面",
                "version_scope": ["项目级 .pmai 隐藏目录初始化", "CLI 任务/提交/决策/日报管理", "内置 Web 查看与编辑", "pip 可安装分发", "移除旧 pmai 路径残留"],
                "avoid_now": ["引入额外后端框架依赖", "依赖团队协作或多用户同步", "把运行时数据放回代码目录"],
                "top_tasks": ["清理旧包名和路径硬编码", "保证 pmai init 可在任意项目创建 .pmai", "保证 pmai-web 使用标准库直接启动"],
                "source_docs": ["README.md", "pyproject.toml", "cli_main.py", "bootstrap.py", "json_store.py", "web_server.py", "run_server.py"],
                "related_decisions": ["decision-runtime-hidden-dir", "decision-stdlib-web-server"],
            },
            "decisions": [
                {"id": "decision-runtime-hidden-dir", "title": "运行时数据放入项目隐藏目录", "date": today(), "status": "accepted", "background": "工具会被复制到不同目录中使用，代码路径不应承担项目数据目录职责。", "decision": "在用户项目根目录使用 .pmai/ 保存数据库和运行配置，并支持从子目录向上发现该目录。", "impact": ["每个项目独立保存数据和端口配置"], "alternatives": ["继续把数据库放在源码目录"], "related_tasks": ["task-runtime-layout"], "updates_canon": True},
                {"id": "decision-stdlib-web-server", "title": "Web 服务改为 Python 标准库", "date": today(), "status": "accepted", "background": "该工具主要由单个开发者在本机使用，不需要额外的 Web 框架复杂度。", "decision": "用 http.server 提供静态文件和 JSON API，去掉额外后端依赖。", "impact": ["安装成本更低", "更适合本地单用户工具"], "alternatives": ["继续保留 FastAPI 方案"], "related_tasks": ["task-stdlib-web"], "updates_canon": True},
            ],
            "tasks": [
                {"id": "task-runtime-layout", "title": "统一 .pmai 运行时目录和初始化逻辑", "status": "in_progress", "priority": "P0", "phase": "foundation", "acceptance": ["pmai init 会创建 .pmai/data/pmai.json 和 .pmai/config.json", "CLI 和 Web 从子目录也能定位到同一项目数据库"], "related_docs": ["bootstrap.py", "store.py", "run_server.py"], "related_decisions": ["decision-runtime-hidden-dir"], "last_note": "", "updated_at": today()},
                {"id": "task-stdlib-web", "title": "将 Web 服务改为标准库实现", "status": "in_progress", "priority": "P0", "phase": "runtime", "acceptance": ["无需额外安装 FastAPI/uvicorn/pydantic", "保留现有前端静态资源和 API 路径"], "related_docs": ["web_server.py", "run_server.py", "pyproject.toml"], "related_decisions": ["decision-stdlib-web-server"], "last_note": "", "updated_at": today()},
            ],
            "ideas": [],
            "doc_records": [
                {"path": "README.md", "type": "readme", "status": "active", "layer": "baseline", "source_of_truth": True, "last_reviewed": today(), "superseded_by": None},
                {"path": "pyproject.toml", "type": "packaging", "status": "active", "layer": "baseline", "source_of_truth": True, "last_reviewed": today(), "superseded_by": None},
                {"path": "cli_main.py", "type": "cli", "status": "active", "layer": "implementation", "source_of_truth": True, "last_reviewed": today(), "superseded_by": None},
                {"path": "bootstrap.py", "type": "bootstrap", "status": "active", "layer": "implementation", "source_of_truth": True, "last_reviewed": today(), "superseded_by": None},
                {"path": "json_store.py", "type": "data-layer", "status": "active", "layer": "implementation", "source_of_truth": True, "last_reviewed": today(), "superseded_by": None},
                {"path": "web_server.py", "type": "web-server", "status": "active", "layer": "implementation", "source_of_truth": True, "last_reviewed": today(), "superseded_by": None},
                {"path": "run_server.py", "type": "runtime-entry", "status": "active", "layer": "implementation", "source_of_truth": True, "last_reviewed": today(), "superseded_by": None},
            ],
            "daily_notes": {},
            "commits": [],
        }
    )
    return False


def fetch_canon(_unused=None):
    try:
        from .json_store import fetch_canon as _fetch_canon
    except ImportError:
        from json_store import fetch_canon as _fetch_canon
    return _fetch_canon()


def replace_canon_item_group(_conn, _item_type, _values):
    return None
