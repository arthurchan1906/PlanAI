"""HTTP server configuration and startup."""
from __future__ import annotations

import json
from http import HTTPStatus
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Dict, List
from urllib.parse import parse_qs, urlparse

from ..store import (
    append_daily_note,
    advance_plan,
    build_brief,
    build_context_pack,
    build_handoff_packet,
    build_next_action_packet,
    create_commit,
    create_decision,
    create_idea,
    create_link,
    create_principle,
    create_plan,
    create_roadmap,
    create_task,
    create_vision,
    delete_link,
    get_commit,
    get_dashboard_summary,
    get_decision,
    get_idea,
    get_principle,
    get_plan,
    get_roadmap,
    get_task,
    get_vision,
    list_commits,
    list_decisions,
    list_ideas,
    list_links,
    list_principles,
    list_plans,
    list_roadmaps,
    list_tasks,
    list_visions,
    replace_daily_note,
    review_idea,
    update_commit,
    update_decision_status,
    update_principle,
    update_plan,
    update_roadmap,
    update_task,
    update_vision,
)
from .handlers import (
    build_web_bootstrap,
    build_web_commit_detail,
    build_web_decision_detail,
    build_web_task_detail,
    handle_audit_docs,
    handle_code_diff,
    handle_code_recent,
    handle_code_status,
    handle_get_canon,
    handle_get_daily_note,
    handle_get_inbox,
    handle_list_daily_notes,
    handle_list_docs,
    handle_create_idea_comment,
    handle_convert_idea,
    handle_update_idea,
    handle_list_ideas,
    handle_list_principles,
    handle_list_tasks,
    handle_list_visions,
    handle_update_canon,
    handle_update_checkpoint,
    handle_update_doc,
    handle_update_task,
)
from .assets import resolve_web_dir

WEB_DIR = resolve_web_dir()


class PMAIRequestHandler(SimpleHTTPRequestHandler):
    """HTTP request handler for PMAI web interface."""

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
                self.send_json(handle_get_canon())
            elif method == 'GET' and path == '/pmai/dashboard':
                self.send_json(get_dashboard_summary())
            elif method == 'GET' and path == '/pmai/inbox':
                self.send_json(handle_get_inbox())
            elif method == 'GET' and path == '/pmai/brief':
                view = (query.get('view') or ['product'])[0]
                self.send_json(build_brief(view))
            elif method == 'GET' and path == '/pmai/context':
                self.send_json(build_context_pack())
            elif method == 'GET' and path == '/pmai/next':
                self.send_json(build_next_action_packet())
            elif method == 'GET' and path == '/pmai/handoff':
                self.send_json(build_handoff_packet())
            elif method == 'GET' and path == '/pmai/code/status':
                self.send_json(handle_code_status())
            elif method == 'GET' and path == '/pmai/code/diff':
                self.send_json(handle_code_diff())
            elif method == 'GET' and path == '/pmai/code/recent':
                limit = int((query.get('limit') or ['10'])[0])
                self.send_json(handle_code_recent(limit))
            elif method == 'POST' and path == '/pmai/canon/update':
                self.send_json(handle_update_canon(self.read_json()))
            elif method == 'GET' and path == '/pmai/visions':
                self.send_json(handle_list_visions())
            elif method == 'POST' and path == '/pmai/visions':
                self.send_json(create_vision(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/visions/'):
                self.send_json(get_vision(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/visions/'):
                self.send_json(update_vision(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/pmai/roadmaps':
                self.send_json({'roadmaps': list_roadmaps((query.get('vision_id') or [None])[0])})
            elif method == 'POST' and path == '/pmai/roadmaps':
                self.send_json(create_roadmap(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/roadmaps/'):
                self.send_json(get_roadmap(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/roadmaps/'):
                self.send_json(update_roadmap(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/pmai/plans':
                self.send_json({'plans': list_plans((query.get('roadmap_id') or [None])[0], (query.get('status') or [None])[0])})
            elif method == 'POST' and path == '/pmai/plans':
                self.send_json(create_plan(self.read_json()))
            elif method == 'POST' and path == '/pmai/plans/generate':
                payload = self.read_json()
                from ..store import generate_plan
                self.send_json(
                    generate_plan(
                        roadmap_id=payload.get('roadmap_id'),
                        vision_id=payload.get('vision_id'),
                        title=payload.get('title', ''),
                        create_tasks_for_plan=bool(payload.get('create_tasks', False)),
                        task_limit=int(payload.get('task_limit', 4)),
                    )
                )
            elif method == 'POST' and path.startswith('/pmai/plans/') and path.endswith('/advance'):
                plan_id = path.split('/')[-2]
                self.send_json(advance_plan(plan_id))
            elif method == 'GET' and path.startswith('/pmai/plans/'):
                self.send_json(get_plan(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/plans/'):
                self.send_json(update_plan(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/pmai/principles':
                self.send_json(handle_list_principles())
            elif method == 'POST' and path == '/pmai/principles':
                self.send_json(create_principle(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/principles/'):
                self.send_json(get_principle(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/principles/'):
                self.send_json(update_principle(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/pmai/links':
                self.send_json(handle_list_links())
            elif method == 'POST' and path == '/pmai/links':
                self.send_json(create_link(self.read_json()))
            elif method == 'DELETE' and path.startswith('/pmai/links/'):
                self.send_json({'ok': delete_link(path.rsplit('/', 1)[-1])})
            elif method == 'GET' and path == '/pmai/tasks':
                self.send_json(
                    handle_list_tasks(
                        (query.get('status') or [None])[0],
                        (query.get('roadmap_id') or [None])[0],
                        (query.get('plan_id') or [None])[0],
                    )
                )
            elif method == 'POST' and path == '/pmai/tasks':
                self.send_json(create_task(self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/web/tasks/'):
                self.send_json(build_web_task_detail(path.rsplit('/', 1)[-1]))
            elif method == 'GET' and path.startswith('/pmai/tasks/'):
                self.send_json(get_task(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/tasks/') and path.endswith('/checkpoint'):
                task_id = path.split('/')[-2]
                payload = self.read_json()
                self.send_json(handle_update_checkpoint(task_id, payload.get('index'), payload.get('done', True)))
            elif method == 'PATCH' and path.startswith('/pmai/tasks/'):
                payload = self.read_json()
                self.send_json(
                    handle_update_task(
                        path.rsplit('/', 1)[-1],
                        payload['status'],
                        payload.get('note', ''),
                        bool(payload.get('allow_without_commit', False)),
                        roadmap_id=payload.get('roadmap_id'),
                        plan_id=payload.get('plan_id'),
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
                self.send_json(handle_list_decisions())
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
            elif method == 'POST' and path.startswith('/pmai/ideas/') and path.endswith('/comments'):
                idea_id = path.split('/')[-2]
                self.send_json(handle_create_idea_comment(idea_id, self.read_json()))
            elif method == 'POST' and path.startswith('/pmai/ideas/') and path.endswith('/convert'):
                idea_id = path.split('/')[-2]
                self.send_json(handle_convert_idea(idea_id, self.read_json()))
            elif method == 'GET' and path.startswith('/pmai/ideas/'):
                self.send_json(get_idea(path.rsplit('/', 1)[-1]))
            elif method == 'PATCH' and path.startswith('/pmai/ideas/'):
                payload = self.read_json()
                idea_id = path.rsplit('/', 1)[-1]
                if "note" in payload and "status" in payload and len(payload.keys()) <= 2:
                    self.send_json(review_idea(idea_id, payload['status'], payload.get('note', '')))
                else:
                    self.send_json(handle_update_idea(idea_id, payload))
            elif method == 'GET' and path == '/pmai/docs':
                self.send_json({'records': list_doc_records((query.get('status') or [None])[0], (query.get('layer') or [None])[0])})
            elif method == 'PATCH' and path == '/pmai/docs':
                self.send_json(handle_update_doc(self.read_json()))
            elif method == 'GET' and path == '/pmai/docs/audit':
                self.send_json(handle_audit_docs())
            elif method == 'GET' and path == '/pmai/daily':
                self.send_json(handle_get_daily_note((query.get('date') or [None])[0]))
            elif method == 'GET' and path == '/pmai/daily/history':
                self.send_json(handle_list_daily_notes())
            elif method == 'POST' and path == '/pmai/daily':
                self.send_json(handle_append_daily_note(self.read_json(), (query.get('date') or [None])[0]))
            elif method == 'PUT' and path == '/pmai/daily':
                self.send_json(replace_daily_note(self.read_json(), (query.get('date') or [None])[0]))
            else:
                return False
            return True
        except Exception as exc:
            self.send_json({'detail': str(exc)}, status=500)
            return True


def create_server(host: str, port: int) -> ThreadingHTTPServer:
    """Create and return a ThreadingHTTPServer instance."""
    return ThreadingHTTPServer((host, port), PMAIRequestHandler)
