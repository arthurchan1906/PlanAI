from __future__ import annotations

import json
from http import HTTPStatus
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any, Dict, List
from urllib.parse import parse_qs, urlparse

from .store import (
    append_daily_note,
    audit_docs,
    build_brief,
    create_commit,
    create_decision,
    create_idea,
    create_link,
    create_principle,
    create_task,
    create_vision,
    delete_link,
    fetch_canon,
    get_commit,
    get_dashboard_summary,
    get_daily_note,
    get_decision,
    get_git_diff,
    get_git_recent_commits,
    get_git_worktree_status,
    get_idea,
    get_inbox_summary,
    get_principle,
    get_task,
    get_vision,
    list_commits,
    list_decisions,
    list_daily_notes,
    list_doc_records,
    list_ideas,
    list_links,
    list_principles,
    list_tasks,
    list_visions,
    replace_daily_note,
    review_idea,
    update_canon,
    update_commit,
    update_decision_status,
    update_doc_record,
    update_principle,
    update_task,
    update_vision,
)

WEB_DIR = Path(__file__).resolve().parent / 'ui' / 'dist'


def _task_status_hint(task: Dict[str, Any], linked_commits: List[Dict[str, Any]]) -> str:
    if task["status"] == "done":
        return "completed"
    if not linked_commits:
        return "needs_commit"
    if not any(item["review_status"] == "approved" for item in linked_commits):
        return "needs_review"
    if any(item["test_status"] != "passed" for item in linked_commits):
        return "needs_verification"
    return "ready"


def _commit_status_hint(commit: Dict[str, Any]) -> str:
    if commit["review_status"] != "approved":
        return "needs_review"
    if commit["test_status"] != "passed":
        return "needs_verification"
    if commit["status"] == "draft":
        return "draft"
    return "ready"


def build_web_task_detail(task_id: str) -> Dict[str, Any]:
    payload = get_task(task_id)
    task = payload["task"]
    linked_commits = payload.get("linked_commits", [])
    links = payload.get("links", {})
    return {
        "task": {
            **task,
            "status_hint": _task_status_hint(task, linked_commits),
        },
        "closure": payload.get("closure", {}),
        "changed_files": payload.get("changed_files", []),
        "linked_commits": [
            {
                **item,
                "short_hash": (item.get("commit_hash") or "")[:8],
                "status_hint": _commit_status_hint(item),
            }
            for item in linked_commits
        ],
        "links": links,
        "link_summary": {
            "outgoing_count": len(links.get("outgoing", [])),
            "incoming_count": len(links.get("incoming", [])),
        },
    }


def build_web_commit_detail(commit_id: str) -> Dict[str, Any]:
    payload = get_commit(commit_id)
    commit = payload["commit"]
    git = payload.get("git", {})
    return {
        "commit": {
            **commit,
            "short_hash": (commit.get("commit_hash") or "")[:8],
            "file_count": len(commit.get("files", [])),
            "status_hint": _commit_status_hint(commit),
        },
        "linked_task": payload.get("linked_task"),
        "linked_decision": payload.get("linked_decision"),
        "links": payload.get("links", {}),
        "git": {
            **git,
            "short_hash": (git.get("commit_hash") or "")[:8],
            "file_count": len(git.get("files", [])),
        },
    }


def build_web_decision_detail(decision_id: str) -> Dict[str, Any]:
    payload = get_decision(decision_id)
    decision = payload["decision"]
    linked_commits = payload.get("linked_commits", [])
    links = payload.get("links", {})
    return {
        "decision": {
            **decision,
            "linked_commit_count": len(linked_commits),
            "related_task_count": len(payload.get("linked_tasks", [])),
        },
        "linked_tasks": payload.get("linked_tasks", []),
        "linked_commits": [
            {
                **item,
                "short_hash": (item.get("commit_hash") or "")[:8],
                "status_hint": _commit_status_hint(item),
            }
            for item in linked_commits
        ],
        "links": links,
        "link_summary": {
            "outgoing_count": len(links.get("outgoing", [])),
            "incoming_count": len(links.get("incoming", [])),
        },
    }


def build_web_bootstrap() -> Dict[str, Any]:
    canon = fetch_canon()
    visions = list_visions()
    principles = list_principles()
    tasks = list_tasks()
    commits = list_commits()
    decisions = list_decisions()
    ideas = list_ideas()
    docs = list_doc_records()
    doc_audit = audit_docs()
    inbox = get_inbox_summary()
    dashboard = get_dashboard_summary()
    code_status = get_git_worktree_status()
    recent_git_commits = get_git_recent_commits(10)
    daily = get_daily_note()

    task_titles = {item["id"]: item["title"] for item in tasks}
    decision_titles = {item["id"]: item["title"] for item in decisions}
    canon_related_decisions = set(canon.get("related_decisions", []))

    commits_by_task: Dict[str, List[Dict[str, Any]]] = {}
    for commit in commits:
        if commit.get("task_id"):
            commits_by_task.setdefault(commit["task_id"], []).append(commit)

    web_tasks = []
    for task in tasks:
        linked_commits = commits_by_task.get(task["id"], [])
        web_tasks.append(
            {
                **task,
                "linked_commit_count": len(linked_commits),
                "approved_commit_count": len([item for item in linked_commits if item["review_status"] == "approved"]),
                "related_decision_titles": [
                    decision_titles[item]
                    for item in task.get("related_decisions", [])
                    if item in decision_titles
                ],
                "status_hint": _task_status_hint(task, linked_commits),
            }
        )

    web_commits = []
    for commit in commits:
        web_commits.append(
            {
                **commit,
                "task_title": task_titles.get(commit.get("task_id") or "", ""),
                "decision_title": decision_titles.get(commit.get("decision_id") or "", ""),
                "short_hash": (commit.get("commit_hash") or "")[:8],
                "file_count": len(commit.get("files", [])),
                "status_hint": _commit_status_hint(commit),
            }
        )

    web_decisions = []
    for decision in decisions:
        web_decisions.append(
            {
                **decision,
                "related_task_titles": [
                    task_titles[item]
                    for item in decision.get("related_tasks", [])
                    if item in task_titles
                ],
                "canon_synced": decision["id"] in canon_related_decisions,
                "linked_commit_count": len([item for item in commits if item.get("decision_id") == decision["id"]]),
            }
        )

    web_docs = []
    invalid_truth_records = set(doc_audit.get("invalid_truth_records", []))
    obsolete_without_replacement = set(doc_audit.get("obsolete_without_replacement", []))
    for doc in docs:
        issues: List[str] = []
        if doc["path"] in invalid_truth_records:
            issues.append("invalid_truth_record")
        if doc["path"] in obsolete_without_replacement:
            issues.append("obsolete_without_replacement")
        web_docs.append({**doc, "issues": issues})

    return {
        "dashboard": dashboard,
        "inbox": inbox,
        "canon": canon,
        "visions": visions,
        "principles": principles,
        "code_status": code_status,
        "recent_git_commits": recent_git_commits,
        "tasks": web_tasks,
        "commits": web_commits,
        "ideas": ideas,
        "docs": web_docs,
        "doc_audit": doc_audit,
        "decisions": web_decisions,
        "daily": daily,
    }


class PMAIRequestHandler(SimpleHTTPRequestHandler):
    extensions_map = {
        **SimpleHTTPRequestHandler.extensions_map,
        ".js": "application/javascript",
        ".mjs": "application/javascript",
        ".css": "text/css",
        ".json": "application/json",
        ".wasm": "application/wasm",
        ".svg": "image/svg+xml",
    }

    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(WEB_DIR), **kwargs)

    def end_headers(self) -> None:
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Access-Control-Allow-Methods', 'GET, POST, PUT, PATCH, DELETE, OPTIONS')
        self.send_header('Access-Control-Allow-Headers', 'Content-Type')
        super().end_headers()

    def do_OPTIONS(self) -> None:
        self.send_response(HTTPStatus.NO_CONTENT)
        self.end_headers()

    def do_GET(self) -> None:
        if self._handle_api('GET'):
            return
        if self.path == '/' or self.path.startswith('/assets/'):
            return super().do_GET()
        self.path = '/index.html'
        return super().do_GET()

    def do_POST(self) -> None:
        if self._handle_api('POST'):
            return
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_PUT(self) -> None:
        if self._handle_api('PUT'):
            return
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_PATCH(self) -> None:
        if self._handle_api('PATCH'):
            return
        self.send_error(HTTPStatus.NOT_FOUND)

    def do_DELETE(self) -> None:
        if self._handle_api('DELETE'):
            return
        self.send_error(HTTPStatus.NOT_FOUND)

    def send_json(self, payload, status: int = 200) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode('utf-8')
        self.send_response(status)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def read_json(self) -> dict:
        length = int(self.headers.get('Content-Length', '0'))
        return {} if length <= 0 else json.loads(self.rfile.read(length).decode('utf-8'))

    def _handle_api(self, method: str) -> bool:
        parsed = urlparse(self.path)
        path = parsed.path
        query = parse_qs(parsed.query)
        try:
            if method == 'GET' and path == '/pmai/web/bootstrap':
                self.send_json(build_web_bootstrap())
            elif method == 'GET' and path == '/pmai/canon':
                self.send_json(fetch_canon())
            elif method == 'GET' and path == '/pmai/dashboard':
                self.send_json(get_dashboard_summary())
            elif method == 'GET' and path == '/pmai/inbox':
                self.send_json(get_inbox_summary())
            elif method == 'GET' and path == '/pmai/brief':
                view = (query.get('view') or ['product'])[0]
                self.send_json(build_brief(view))
            elif method == 'GET' and path == '/pmai/code/status':
                self.send_json(get_git_worktree_status())
            elif method == 'GET' and path == '/pmai/code/diff':
                self.send_json({'diff': get_git_diff()})
            elif method == 'GET' and path == '/pmai/code/recent':
                limit = int((query.get('limit') or ['10'])[0])
                self.send_json({'commits': get_git_recent_commits(limit)})
            elif method == 'POST' and path == '/pmai/canon/update':
                self.send_json(update_canon(self.read_json()))
            elif method == 'GET' and path == '/pmai/visions':
                self.send_json({'visions': list_visions()})
            elif method == 'POST' and path == '/pmai/visions':
                self.send_json(create_vision(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/visions/'):
                self.send_json(get_vision(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/visions/'):
                self.send_json(update_vision(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/pmai/principles':
                self.send_json({'principles': list_principles()})
            elif method == 'POST' and path == '/pmai/principles':
                self.send_json(create_principle(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/principles/'):
                self.send_json(get_principle(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/principles/'):
                self.send_json(update_principle(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/pmai/links':
                self.send_json({'links': list_links()})
            elif method == 'POST' and path == '/pmai/links':
                self.send_json(create_link(self.read_json()))
            elif method == 'DELETE' and path.startswith('/pmai/links/'):
                self.send_json({'ok': delete_link(path.rsplit('/', 1)[-1])})
            elif method == 'GET' and path == '/pmai/tasks':
                self.send_json({'tasks': list_tasks((query.get('status') or [None])[0])})
            elif method == 'POST' and path == '/pmai/tasks':
                self.send_json(create_task(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/web/tasks/'):
                self.send_json(build_web_task_detail(path.rsplit('/', 1)[-1]))
            elif method == 'GET' and path.startswith('/pmai/tasks/'):
                self.send_json(get_task(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/tasks/'):
                payload = self.read_json()
                self.send_json(
                    update_task(
                        path.rsplit('/', 1)[-1],
                        payload['status'],
                        payload.get('note', ''),
                        bool(payload.get('allow_without_commit', False)),
                    )
                )
            elif method == 'GET' and path == '/pmai/commits':
                self.send_json(
                    {
                        'commits': list_commits(
                            (query.get('status') or [None])[0],
                            (query.get('task_id') or [None])[0],
                            (query.get('decision_id') or [None])[0],
                        )
                    }
                )
            elif method == 'POST' and path == '/pmai/commits':
                self.send_json(create_commit(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/web/commits/'):
                self.send_json(build_web_commit_detail(path.rsplit('/', 1)[-1]))
            elif method == 'GET' and path.startswith('/pmai/commits/'):
                self.send_json(get_commit(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/commits/'):
                self.send_json(update_commit(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/pmai/decisions':
                self.send_json({'decisions': list_decisions()})
            elif method == 'POST' and path == '/pmai/decisions':
                self.send_json(create_decision(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/web/decisions/'):
                self.send_json(build_web_decision_detail(path.rsplit('/', 1)[-1]))
            elif method == 'GET' and path.startswith('/pmai/decisions/'):
                self.send_json(get_decision(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/decisions/'):
                self.send_json(update_decision_status(path.rsplit('/', 1)[-1], self.read_json()['status']))
            elif method == 'GET' and path == '/pmai/ideas':
                self.send_json({'ideas': list_ideas((query.get('status') or [None])[0])})
            elif method == 'POST' and path == '/pmai/ideas':
                self.send_json(create_idea(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/ideas/'):
                self.send_json(get_idea(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/ideas/'):
                payload = self.read_json(); self.send_json(review_idea(path.rsplit('/', 1)[-1], payload['status'], payload.get('note', '')))
            elif method == 'GET' and path == '/pmai/docs':
                self.send_json({'records': list_doc_records((query.get('status') or [None])[0], (query.get('layer') or [None])[0])})
            elif method == 'PATCH' and path == '/pmai/docs':
                self.send_json(update_doc_record(self.read_json()))
            elif method == 'GET' and path == '/pmai/docs/audit':
                self.send_json(audit_docs())
            elif method == 'GET' and path == '/pmai/daily':
                self.send_json(get_daily_note((query.get('date') or [None])[0]))
            elif method == 'GET' and path == '/pmai/daily/history':
                self.send_json({'items': list_daily_notes()})
            elif method == 'POST' and path == '/pmai/daily':
                self.send_json(append_daily_note(self.read_json(), (query.get('date') or [None])[0]))
            elif method == 'PUT' and path == '/pmai/daily':
                self.send_json(replace_daily_note(self.read_json(), (query.get('date') or [None])[0]))
            else:
                return False
            return True
        except Exception as exc:
            self.send_json({'detail': str(exc)}, status=500)
            return True


def create_server(host: str, port: int) -> ThreadingHTTPServer:
    return ThreadingHTTPServer((host, port), PMAIRequestHandler)
