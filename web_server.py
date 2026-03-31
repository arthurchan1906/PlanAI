from __future__ import annotations

import json
from http import HTTPStatus
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

try:
    from .json_store import (
        append_daily_note,
        audit_docs,
        create_commit,
        create_decision,
        create_idea,
        create_task,
        fetch_canon,
        get_dashboard_summary,
        get_daily_note,
        list_commits,
        list_decisions,
        list_daily_notes,
        list_doc_records,
        list_ideas,
        list_tasks,
        replace_daily_note,
        review_idea,
        update_canon,
        update_commit,
        update_decision_status,
        update_doc_record,
        update_task,
    )
except ImportError:
    from json_store import (
        append_daily_note,
        audit_docs,
        create_commit,
        create_decision,
        create_idea,
        create_task,
        fetch_canon,
        get_dashboard_summary,
        get_daily_note,
        list_commits,
        list_decisions,
        list_daily_notes,
        list_doc_records,
        list_ideas,
        list_tasks,
        replace_daily_note,
        review_idea,
        update_canon,
        update_commit,
        update_decision_status,
        update_doc_record,
        update_task,
    )

WEB_DIR = Path(__file__).resolve().parent / 'ui' / 'dist'


class PlanAIRequestHandler(SimpleHTTPRequestHandler):
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
        self.send_header('Access-Control-Allow-Methods', 'GET, POST, PUT, PATCH, OPTIONS')
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
            if method == 'GET' and path == '/planai/canon':
                self.send_json(fetch_canon())
            elif method == 'GET' and path == '/planai/dashboard':
                self.send_json(get_dashboard_summary())
            elif method == 'POST' and path == '/planai/canon/update':
                self.send_json(update_canon(self.read_json()))
            elif method == 'GET' and path == '/planai/tasks':
                self.send_json({'tasks': list_tasks()})
            elif method == 'POST' and path == '/planai/tasks':
                self.send_json(create_task(self.read_json()))
            elif method == 'PATCH' and path.startswith('/planai/tasks/'):
                payload = self.read_json(); self.send_json(update_task(path.rsplit('/', 1)[-1], payload['status'], payload.get('note', '')))
            elif method == 'GET' and path == '/planai/commits':
                self.send_json({'commits': list_commits((query.get('status') or [None])[0])})
            elif method == 'POST' and path == '/planai/commits':
                self.send_json(create_commit(self.read_json()))
            elif method == 'PATCH' and path.startswith('/planai/commits/'):
                self.send_json(update_commit(path.rsplit('/', 1)[-1], self.read_json()))
            elif method == 'GET' and path == '/planai/decisions':
                self.send_json({'decisions': list_decisions()})
            elif method == 'POST' and path == '/planai/decisions':
                self.send_json(create_decision(self.read_json()))
            elif method == 'PATCH' and path.startswith('/planai/decisions/'):
                self.send_json(update_decision_status(path.rsplit('/', 1)[-1], self.read_json()['status']))
            elif method == 'GET' and path == '/planai/ideas':
                self.send_json({'ideas': list_ideas((query.get('status') or [None])[0])})
            elif method == 'POST' and path == '/planai/ideas':
                self.send_json(create_idea(self.read_json()))
            elif method == 'PATCH' and path.startswith('/planai/ideas/'):
                payload = self.read_json(); self.send_json(review_idea(path.rsplit('/', 1)[-1], payload['status'], payload.get('note', '')))
            elif method == 'GET' and path == '/planai/docs':
                self.send_json({'records': list_doc_records((query.get('status') or [None])[0], (query.get('layer') or [None])[0])})
            elif method == 'PATCH' and path == '/planai/docs':
                self.send_json(update_doc_record(self.read_json()))
            elif method == 'GET' and path == '/planai/docs/audit':
                self.send_json(audit_docs())
            elif method == 'GET' and path == '/planai/daily':
                self.send_json(get_daily_note((query.get('date') or [None])[0]))
            elif method == 'GET' and path == '/planai/daily/history':
                self.send_json({'items': list_daily_notes()})
            elif method == 'POST' and path == '/planai/daily':
                self.send_json(append_daily_note(self.read_json(), (query.get('date') or [None])[0]))
            elif method == 'PUT' and path == '/planai/daily':
                self.send_json(replace_daily_note(self.read_json(), (query.get('date') or [None])[0]))
            else:
                return False
            return True
        except Exception as exc:
            self.send_json({'detail': str(exc)}, status=500)
            return True


def create_server(host: str, port: int) -> ThreadingHTTPServer:
    return ThreadingHTTPServer((host, port), PlanAIRequestHandler)

