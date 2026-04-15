"""Request handlers for pmai web API endpoints."""
from __future__ import annotations

from .bootstrap import build_web_bootstrap
from .canon import handle_get_canon, handle_update_canon
from .code import handle_code_diff, handle_code_status, handle_code_recent
from .commits import build_web_commit_detail, handle_create_commit, handle_get_commit, handle_list_commits, handle_update_commit
from .daily import handle_append_daily_note, handle_get_daily_note, handle_list_daily_notes, handle_replace_daily_note
from .decisions import build_web_decision_detail, handle_create_decision, handle_get_decision, handle_list_decisions, handle_update_decision_status_handler
from .docs import handle_audit_docs, handle_list_docs, handle_update_doc, handle_sync_docs, handle_prune_docs, handle_get_doc_content
from .ideas import (
    handle_create_idea,
    handle_create_idea_comment,
    handle_convert_idea,
    handle_get_idea,
    handle_list_ideas,
    handle_review_idea,
    handle_update_idea,
)
from .inbox import handle_get_inbox
from .links import handle_create_link, handle_delete_link, handle_list_links
from .principles import handle_create_principle, handle_get_principle, handle_list_principles, handle_update_principle
from .tasks import build_web_task_detail, handle_create_task, handle_get_task, handle_list_tasks, handle_update_checkpoint, handle_update_task
from .visions import handle_create_vision, handle_get_vision, handle_list_visions, handle_update_vision

__all__ = [
    # Bootstrap
    "build_web_bootstrap",
    # Canon
    "handle_get_canon",
    "handle_update_canon",
    # Code
    "handle_code_status",
    "handle_code_diff",
    "handle_code_recent",
    # Commits
    "handle_list_commits",
    "handle_create_commit",
    "handle_get_commit",
    "update_commit",
    "build_web_commit_detail",
    # Daily
    "handle_get_daily_note",
    "handle_list_daily_notes",
    "handle_append_daily_note",
    "handle_replace_daily_note",
    # Decisions
    "handle_list_decisions",
    "handle_create_decision",
    "handle_get_decision",
    "handle_update_decision_status_handler",
    "build_web_decision_detail",
    # Docs
    "handle_list_docs",
    "handle_update_doc",
    "handle_audit_docs",
    "handle_sync_docs",
    "handle_prune_docs",
    "handle_get_doc_content",
    # Ideas
    "handle_list_ideas",
    "handle_create_idea",
    "handle_get_idea",
    "handle_review_idea",
    "handle_update_idea",
    "handle_create_idea_comment",
    "handle_convert_idea",
    # Inbox
    "handle_get_inbox",
    # Links
    "handle_list_links",
    "handle_create_link",
    "handle_delete_link",
    # Principles
    "handle_list_principles",
    "handle_create_principle",
    "handle_get_principle",
    "handle_update_principle",
    # Tasks
    "handle_list_tasks",
    "handle_create_task",
    "handle_get_task",
    "handle_update_task",
    "handle_update_checkpoint",
    "build_web_task_detail",
    # Visions
    "handle_list_visions",
    "handle_create_vision",
    "handle_get_vision",
    "handle_update_vision",
]
