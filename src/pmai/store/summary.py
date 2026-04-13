from __future__ import annotations

from typing import Any, Dict, List

from .canon import fetch_canon
from .tasks import list_tasks
from .ideas import list_ideas
from .decisions import list_decisions
from .docs import list_doc_records, audit_docs
from .commits import list_commits
from .daily import get_daily_note
from .visions import get_active_vision
from .principles import list_active_principles
from .plans import list_plans
from .links import list_links
from .git import get_git_worktree_status


def get_dashboard_summary() -> Dict[str, Any]:
    canon = fetch_canon()
    tasks = list_tasks()
    ideas = list_ideas()
    decisions = list_decisions()
    docs = list_doc_records()
    commits = list_commits()
    plans = list_plans()
    daily = get_daily_note()
    current_recommendations = daily["next"][:4]
    current_risks = daily["risks"][:3]
    plan_attention = [
        {
            "id": plan["id"],
            "title": plan["title"],
            "state": plan.get("health", {}).get("state"),
            "issues": plan.get("health", {}).get("issues", []),
            "next_manager_checkpoint": plan.get("manager_summary", {}).get("next_manager_checkpoint", ""),
            "recommendations": plan.get("recommendations", [])[:2],
            "auto_action_available": bool(plan.get("recommendations") and plan["recommendations"][0].get("auto_supported")),
            "manager_review_required": bool(
                plan.get("health", {}).get("needs_manager_attention")
                and not (plan.get("recommendations") and plan["recommendations"][0].get("auto_supported"))
            ),
        }
        for plan in plans
        if plan.get("health", {}).get("needs_manager_attention")
    ][:5]
    return {
        "canon_updated_at": canon["updated_at"],
        "task_counts": {
            "total": len(tasks),
            "in_progress": len([task for task in tasks if task["status"] == "in_progress"]),
            "todo": len([task for task in tasks if task["status"] == "todo"]),
            "blocked": len([task for task in tasks if task["status"] == "blocked"]),
        },
        "idea_counts": {
            "total": len(ideas),
            "inbox": len([idea for idea in ideas if idea["status"] == "inbox"]),
            "accepted": len([idea for idea in ideas if idea["status"] == "accepted"]),
        },
        "decision_counts": {
            "total": len(decisions),
            "accepted": len([decision for decision in decisions if decision["status"] == "accepted"]),
            "proposed": len([decision for decision in decisions if decision["status"] == "proposed"]),
        },
        "doc_counts": {
            "total": len(docs),
            "active": len([doc for doc in docs if doc["status"] == "active"]),
            "source_of_truth": len([doc for doc in docs if doc["source_of_truth"]]),
        },
        "commit_counts": {
            "total": len(commits),
            "draft": len([commit for commit in commits if commit["status"] == "draft"]),
            "committed": len([commit for commit in commits if commit["status"] == "committed"]),
            "merged": len([commit for commit in commits if commit["status"] == "merged"]),
            "needs_review": len([commit for commit in commits if commit["review_status"] != "approved"]),
        },
        "plan_counts": {
            "total": len(plans),
            "active": len([plan for plan in plans if plan["status"] == "active"]),
            "draft": len([plan for plan in plans if plan["status"] == "draft"]),
            "generated": len([plan for plan in plans if plan.get("source") == "generated"]),
            "without_tasks": len([plan for plan in plans if not plan.get("task_ids")]),
            "auto_advance_ready": len(
                [plan for plan in plans if plan.get("recommendations") and plan["recommendations"][0].get("auto_supported")]
            ),
            "manager_review_required": len(
                [
                    plan
                    for plan in plans
                    if plan.get("health", {}).get("needs_manager_attention")
                    and not (plan.get("recommendations") and plan["recommendations"][0].get("auto_supported"))
                ]
            ),
            "with_open_tasks": len(
                [
                    plan
                    for plan in plans
                    if any(task.get("status") != "done" for task in plan.get("linked_tasks", []))
                ]
            ),
        },
        "today_focus": [
            *[task["title"] for task in tasks if task["status"] == "in_progress"][:3],
            *daily["next"][:3],
        ][:5],
        "current_recommendations": current_recommendations,
        "current_risks": current_risks,
        "plan_attention": plan_attention,
        "recent_commits": commits[:5],
    }


def _module_guess_from_task(task: Dict[str, Any]) -> str:
    raw_title = task.get("title") or ""
    title = raw_title.lower()
    phase = (task.get("phase") or "").lower()
    analytics_keywords = ["埋点", "数据", "画像", "智能", "assessment", "feedback"]
    ai_keywords = ["AI", "ai", "教练", "对话", "元认知", "coach"]
    review_keywords = ["复盘", "review", "remediation"]
    paper_keywords = ["题目", "试卷", "paper", "question"]
    if any(keyword in raw_title for keyword in analytics_keywords):
        return "analytics"
    if any(keyword in raw_title for keyword in ai_keywords):
        return "ai_core"
    if any(keyword in raw_title for keyword in review_keywords):
        return "review"
    if any(keyword in raw_title for keyword in paper_keywords):
        return "papers"
    if phase in {"analytics", "review", "papers", "ai_core"}:
        return phase
    if phase:
        return f"phase:{phase}"
    return "general"


def build_brief(view: str) -> Dict[str, Any]:
    vision = get_active_vision()
    principles = list_active_principles(limit=8)
    canon = fetch_canon()
    tasks = list_tasks()
    roadmaps = _list_roadmaps_for_brief()
    in_progress = [task for task in tasks if task["status"] == "in_progress"]
    commits = list_commits()[:5]
    daily = get_daily_note()
    links = list_links()
    docs = list_doc_records(status="active")
    git_status = get_git_worktree_status()
    decisions = list_decisions()
    accepted_decisions = [item for item in decisions if item["status"] == "accepted"][:5]

    if view == "product":
        return {
            "view": "product",
            "goal": vision,
            "principles": principles,
            "roadmaps": roadmaps,
            "current_stage": {
                "engineering_focus": canon.get("engineering_focus", ""),
                "architecture": canon.get("architecture", ""),
            },
            "active_workstreams": [
                {
                    "id": task["id"],
                    "title": task["title"],
                    "priority": task["priority"],
                    "phase": task["phase"],
                }
                for task in in_progress[:6]
            ],
            "accepted_decisions": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "date": item["date"],
                }
                for item in accepted_decisions
            ],
            "today": {
                "completed": daily.get("completed", [])[:5],
                "risks": daily.get("risks", [])[:5],
                "next": daily.get("next", [])[:5],
            },
        }

    if view == "architecture":
        return {
            "view": "architecture",
            "goal": vision,
            "principles": principles,
            "accepted_decisions": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "date": item["date"],
                    "updates_canon": item["updates_canon"],
                }
                for item in accepted_decisions
            ],
            "system_layers": [
                {
                    "name": "governance",
                    "objects": ["vision", "principle", "decision", "task", "commit", "link"],
                },
                {
                    "name": "runtime_code",
                    "objects": ["code status", "code diff", "code recent"],
                },
                {
                    "name": "project_runtime",
                    "objects": ["daily", "inbox", "canon", "docs"],
                },
            ],
            "current_boundaries": {
                "canon": canon,
                "git": {
                    "branch": git_status.get("branch"),
                    "dirty": git_status.get("dirty"),
                    "changed_files_count": git_status.get("changed_files_count"),
                },
            },
            "recent_evidence": {
                "commits": commits,
                "links": links[:8],
                "active_docs": docs[:8],
            },
        }

    if view == "modules":
        grouped: Dict[str, List[Dict[str, Any]]] = {}
        for task in in_progress:
            grouped.setdefault(_module_guess_from_task(task), []).append(task)
        modules = []
        for name, items in sorted(grouped.items(), key=lambda pair: pair[0]):
            task_ids = {task["id"] for task in items}
            module_commits = [commit for commit in commits if commit.get("task_id") in task_ids]
            modules.append(
                {
                    "module": name,
                    "tasks": [
                        {
                            "id": task["id"],
                            "title": task["title"],
                            "priority": task["priority"],
                            "status": task["status"],
                        }
                        for task in items
                    ],
                    "recent_commits": [
                        {
                            "id": commit["id"],
                            "title": commit["title"],
                            "task_id": commit.get("task_id"),
                        }
                        for commit in module_commits[:3]
                    ],
                }
            )
        return {
            "view": "modules",
            "goal": vision,
            "accepted_decisions": [
                {
                    "id": item["id"],
                    "title": item["title"],
                    "date": item["date"],
                }
                for item in accepted_decisions
            ],
            "modules": modules,
            "recent_commits": commits,
            "links": links[:10],
        }

    raise KeyError(view)


def get_inbox_summary() -> Dict[str, Any]:
    canon = fetch_canon()
    tasks = list_tasks()
    decisions = list_decisions()
    commits = list_commits()
    doc_audit = audit_docs()
    canon_related_decisions = set(canon.get("related_decisions", []))

    proposed_decisions = [item for item in decisions if item["status"] == "proposed"]
    canon_followups = [
        item
        for item in decisions
        if item["status"] == "accepted"
        and item["updates_canon"]
        and item["id"] not in canon_related_decisions
    ]
    review_commits = [
        item
        for item in commits
        if item["status"] in ("committed", "merged") and item["review_status"] != "approved"
    ]
    verification_gaps = [
        item
        for item in commits
        if item["status"] in ("committed", "merged")
        and (item["test_status"] != "passed" or item["review_status"] != "approved")
    ]
    task_closure_blockers: List[Dict[str, Any]] = []
    for task in tasks:
        if task["status"] not in ("in_progress", "blocked"):
            continue
        linked_commits = [item for item in commits if item["task_id"] == task["id"]]
        approved_linked_commits = [
            item
            for item in linked_commits
            if item["status"] in ("committed", "merged") and item["review_status"] == "approved"
        ]
        blocker_reasons: List[str] = []
        if not linked_commits:
            blocker_reasons.append("no_linked_commit")
        if linked_commits and not approved_linked_commits:
            blocker_reasons.append("no_approved_commit")
        if any(item["test_status"] != "passed" for item in linked_commits):
            blocker_reasons.append("verification_incomplete")
        if blocker_reasons:
            task_closure_blockers.append(
                {
                    "id": task["id"],
                    "title": task["title"],
                    "status": task["status"],
                    "priority": task["priority"],
                    "phase": task["phase"],
                    "reasons": blocker_reasons,
                    "linked_commit_ids": [item["id"] for item in linked_commits],
                }
            )
    blocking_doc_issues: List[Dict[str, Any]] = []
    for path in doc_audit.get("obsolete_without_replacement", []):
        blocking_doc_issues.append(
            {
                "type": "obsolete_without_replacement",
                "path": path,
            }
        )
    for path in doc_audit.get("invalid_truth_records", []):
        blocking_doc_issues.append(
            {
                "type": "invalid_truth_records",
                "path": path,
            }
        )

    recommended_actions: List[Dict[str, Any]] = []
    for item in proposed_decisions[:3]:
        recommended_actions.append(
            {
                "kind": "decision_review",
                "priority": "high",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc decision review --id {item['id']} --status accepted",
                "reason": "Proposed decisions should be explicitly accepted or rejected before downstream work continues.",
            }
        )
    for item in canon_followups[:3]:
        recommended_actions.append(
            {
                "kind": "canon_followup",
                "priority": "high",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc canon update --decision-id {item['id']}",
                "reason": "Accepted decisions marked as canon-affecting should be reflected in canon explicitly.",
            }
        )
    for item in review_commits[:3]:
        recommended_actions.append(
            {
                "kind": "commit_review",
                "priority": "medium",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc commit update --id {item['id']} --review-status approved",
                "reason": "Committed changes should be reviewed so related tasks can close without manual guesswork.",
            }
        )
    for item in verification_gaps[:3]:
        recommended_actions.append(
            {
                "kind": "verification_gap",
                "priority": "medium",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc commit update --id {item['id']} --test-status passed --review-status approved",
                "reason": "Committed changes without passing verification or review should not be treated as closure-ready.",
            }
        )
    for item in task_closure_blockers[:3]:
        recommended_actions.append(
            {
                "kind": "task_closure_blocker",
                "priority": "medium",
                "target_id": item["id"],
                "title": item["title"],
                "command": f"aipmc commit list --task-id {item['id']}",
                "reason": f"Task remains closure-blocked because: {', '.join(item['reasons'])}.",
            }
        )
    for item in blocking_doc_issues[:3]:
        recommended_actions.append(
            {
                "kind": "doc_governance",
                "priority": "medium",
                "target_id": item["path"],
                "title": item["path"],
                "command": "aipmc docs audit",
                "reason": f"Document issue `{item['type']}` needs explicit cleanup to keep source-of-truth status reliable.",
            }
        )

    return {
        "canon": {
            "updated_at": canon.get("updated_at"),
            "related_decisions_count": len(canon_related_decisions),
        },
        "counts": {
            "proposed_decisions": len(proposed_decisions),
            "canon_followups": len(canon_followups),
            "review_commits": len(review_commits),
            "verification_gaps": len(verification_gaps),
            "task_closure_blockers": len(task_closure_blockers),
            "doc_issues": len(blocking_doc_issues),
            "total": (
                len(proposed_decisions)
                + len(canon_followups)
                + len(review_commits)
                + len(verification_gaps)
                + len(task_closure_blockers)
                + len(blocking_doc_issues)
            ),
        },
        "pending_items": {
            "proposed_decisions": proposed_decisions,
            "canon_followups": canon_followups,
            "review_commits": review_commits,
            "verification_gaps": verification_gaps,
            "task_closure_blockers": task_closure_blockers,
            "doc_issues": blocking_doc_issues,
        },
        "recommended_actions": recommended_actions,
    }


def get_status_snapshot() -> Dict[str, Any]:
    from .tasks import list_tasks
    from .daily import get_daily_note
    from .commits import list_commits
    from .git import get_git_worktree_status, list_recent_git_commits
    from .visions import get_active_vision
    from .principles import list_active_principles
    from .config import describe_runtime

    in_progress_tasks = list_tasks("in_progress")
    daily = get_daily_note()
    inbox = get_inbox_summary()
    pmai_commits = list_commits()[:3]
    git_status = get_git_worktree_status()
    git_recent = list_recent_git_commits(3)
    active_vision = get_active_vision()
    active_principles = list_active_principles()
    return {
        "project": describe_runtime(),
        "vision": active_vision,
        "principles": active_principles,
        "entrypoints": {
            "product": "aipmc brief product",
            "architecture": "aipmc brief architecture",
            "modules": "aipmc brief modules",
            "code": "aipmc code status",
        },
        "tasks": {
            "in_progress_count": len(in_progress_tasks),
            "in_progress": in_progress_tasks[:5],
        },
        "daily": daily,
        "inbox": {
            "counts": inbox.get("counts", {}),
            "recommended_actions": inbox.get("recommended_actions", [])[:5],
        },
        "recent_commits": {
            "pmai": pmai_commits,
            "git": git_recent,
        },
        "git": git_status,
    }


def _list_roadmaps_for_brief() -> List[Dict[str, Any]]:
    try:
        from .roadmaps import list_roadmaps
        return list_roadmaps()
    except ImportError:
        return []
