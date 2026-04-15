"""
pmai.store 包 - 数据存储层

该包提供了所有数据管理相关的功能，包括：
- 配置管理 (config)
- 数据库连接与 schema (db)
- Git 集成 (git)
- 任务管理 (tasks)
- Roadmap 管理 (roadmaps)
- 提交管理 (commits)
- 决策管理 (decisions)
- 想法管理 (ideas)
- 愿景管理 (visions)
- 原则管理 (principles)
- 链接关系 (links)
- 文档管理 (docs)
- 日报管理 (daily)
- Canon 管理 (canon)
- 汇总查询 (summary)
"""

from __future__ import annotations

# 配置管理
from .config import (
    find_runtime_dir,
    get_project_root,
    get_runtime_dir,
    get_db_path,
    get_journal_path,
    get_config_path,
    load_runtime_config,
    write_runtime_config,
    save_runtime_config,
    describe_runtime,
    RUNTIME_DIRNAME,
    DB_FILENAME,
    CONFIG_FILENAME,
    DEFAULT_WEB_HOST,
    DEFAULT_WEB_PORT,
)

# 数据库连接与 schema
from .db import (
    ensure_runtime_schema,
    get_connection,
    bootstrap_db,
    bootstrap_database,
    RECOVERY_DIR,
    RECOVERED_DB_PATH,
)

# Git 集成
from .git import (
    get_git_worktree_status,
    get_git_diff,
    get_git_diff_summary,
    get_git_recent_commits,
    list_recent_git_commits,
    _get_git_commit_snapshot,
    infer_git_metadata,
    _normalize_git_path,
    _normalize_file_list,
)

# 任务管理
from .tasks import (
    list_tasks,
    get_task,
    create_task,
    update_task,
    plan_task,
    update_task_checkpoint,
    get_module_progress,
)

# Roadmap 管理
from .roadmaps import (
    list_roadmaps,
    get_roadmap,
    create_roadmap,
    update_roadmap,
)

# Plan 绠＄悊
from .plans import (
    list_plans,
    get_plan,
    create_plan,
    update_plan,
    generate_plan,
    advance_plan,
)

# 提交管理
from .commits import (
    list_commits,
    get_commit,
    create_commit,
    update_commit,
)

# 决策管理
from .decisions import (
    list_decisions,
    get_decision,
    create_decision,
    update_decision_status,
)

# 想法管理
from .ideas import (
    list_ideas,
    get_idea,
    create_idea,
    review_idea,
    update_idea,
)

from .idea_comments import (
    list_idea_comments,
    create_idea_comment,
)

from .idea_conversion import (
    convert_idea,
    get_idea_conversion,
    enrich_idea_with_conversion,
)

# 愿景管理
from .visions import (
    list_visions,
    get_vision,
    get_active_vision,
    create_vision,
    update_vision,
)

# 原则管理
from .principles import (
    list_principles,
    get_principle,
    get_active_principles,
    list_active_principles,
    create_principle,
    update_principle,
)

# 链接关系
from .links import (
    list_links,
    list_links_for_entity,
    create_link,
    delete_link,
)

# 文档管理
from .docs import (
    list_doc_records,
    update_doc_record,
    audit_docs,
    read_doc_content,
)

# 日报管理
from .daily import (
    get_daily_note,
    list_daily_notes,
    append_daily_note,
    replace_daily_note,
)

# Canon 管理
from .canon import (
    fetch_canon,
    update_canon,
    empty_canon,
)

# 汇总查询
from .summary import (
    get_dashboard_summary,
    get_inbox_summary,
    build_brief,
    get_status_snapshot,
    _module_guess_from_task,
)

# Context runtime
from .context_runtime import (
    build_context_pack,
    build_next_action_packet,
    build_handoff_packet,
)

# 辅助函数
from .tasks import (
    now_iso,
    today,
    slug,
    dumps,
    loads,
)

__all__ = [
    # 配置管理
    "find_runtime_dir",
    "get_project_root",
    "get_runtime_dir",
    "get_db_path",
    "get_journal_path",
    "get_config_path",
    "load_runtime_config",
    "write_runtime_config",
    "save_runtime_config",
    "describe_runtime",
    "RUNTIME_DIRNAME",
    "DB_FILENAME",
    "CONFIG_FILENAME",
    "DEFAULT_WEB_HOST",
    "DEFAULT_WEB_PORT",
    # 数据库
    "ensure_runtime_schema",
    "get_connection",
    "bootstrap_db",
    "bootstrap_database",
    "RECOVERY_DIR",
    "RECOVERED_DB_PATH",
    # Git
    "get_git_worktree_status",
    "get_git_diff",
    "get_git_diff_summary",
    "get_git_recent_commits",
    "list_recent_git_commits",
    "_get_git_commit_snapshot",
    "infer_git_metadata",
    "_normalize_git_path",
    "_normalize_file_list",
    # 任务
    "list_tasks",
    "get_task",
    "create_task",
    "update_task",
    "plan_task",
    "update_task_checkpoint",
    "get_module_progress",
    # Roadmap
    "list_roadmaps",
    "get_roadmap",
    "create_roadmap",
    "update_roadmap",
    "list_plans",
    "get_plan",
    "create_plan",
    "update_plan",
    "generate_plan",
    "advance_plan",
    # 提交
    "list_commits",
    "get_commit",
    "create_commit",
    "update_commit",
    # 决策
    "list_decisions",
    "get_decision",
    "create_decision",
    "update_decision_status",
    # 想法
    "list_ideas",
    "get_idea",
    "create_idea",
    "review_idea",
    "update_idea",
    "list_idea_comments",
    "create_idea_comment",
    "convert_idea",
    "get_idea_conversion",
    "enrich_idea_with_conversion",
    # 愿景
    "list_visions",
    "get_vision",
    "get_active_vision",
    "create_vision",
    "update_vision",
    # 原则
    "list_principles",
    "get_principle",
    "get_active_principles",
    "list_active_principles",
    "create_principle",
    "update_principle",
    # 链接
    "list_links",
    "list_links_for_entity",
    "create_link",
    "delete_link",
    # 文档
    "list_doc_records",
    "update_doc_record",
    "audit_docs",
    "read_doc_content",
    # 日报
    "get_daily_note",
    "list_daily_notes",
    "append_daily_note",
    "replace_daily_note",
    # Canon
    "fetch_canon",
    "update_canon",
    "empty_canon",
    # 汇总
    "get_dashboard_summary",
    "get_inbox_summary",
    "build_brief",
    "get_status_snapshot",
    "_module_guess_from_task",
    # Context runtime
    "build_context_pack",
    "build_next_action_packet",
    "build_handoff_packet",
    # 辅助
    "now_iso",
    "today",
    "slug",
    "dumps",
    "loads",
]
