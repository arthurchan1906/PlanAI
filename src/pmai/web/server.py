"""HTTP server configuration and startup."""
from __future__ import annotations

import json
from http import HTTPStatus
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Dict, List
from urllib.parse import parse_qs, urlparse

from ..store import (
    append_daily_note,
    append_task_note,
    advance_plan,
    delete_task_note,
    build_brief,
    create_bug,
    build_context_pack,
    build_handoff_packet,
    build_next_action_packet,
    create_commit,
    create_decision,
    get_bug,
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
    list_bugs,
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
    update_bug,
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
    build_web_bug_detail,
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
    handle_sync_docs,
    handle_repair_docs,
    handle_prune_docs,
    handle_get_doc_content,
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
            handler = self._match_route(method, path, query)
            if handler is None:
                return False
            handler(query, path)
            return True
        except Exception as exc:
            self.send_json({'detail': str(exc)}, status=500)
            return True

    def _match_route(self, method: str, path: str, query):
        """Returns a handler fn(query, path) or None."""
        q = self  # capture self for nested handlers

        # -- Exact-match routes --
        _exact = {
            ('GET', '/pmai/web/bootstrap'):   lambda q, p: q.send_json(build_web_bootstrap()),
            ('GET', '/pmai/search'):          lambda q, p: _search(q, p),
            ('GET', '/pmai/canon'):           lambda q, p: q.send_json(handle_get_canon()),
            ('GET', '/pmai/dashboard'):       lambda q, p: q.send_json(get_dashboard_summary()),
            ('GET', '/pmai/inbox'):           lambda q, p: q.send_json(handle_get_inbox()),
            ('GET', '/pmai/brief'):           lambda q, p: q.send_json(build_brief((_qp(q, p, 'view') or ['product'])[0])),
            ('GET', '/pmai/context'):         lambda q, p: q.send_json(build_context_pack()),
            ('GET', '/pmai/next'):            lambda q, p: q.send_json(build_next_action_packet()),
            ('GET', '/pmai/handoff'):         lambda q, p: q.send_json(build_handoff_packet()),
            ('GET', '/pmai/code/status'):     lambda q, p: q.send_json(handle_code_status()),
            ('GET', '/pmai/code/diff'):       lambda q, p: q.send_json(handle_code_diff()),
            ('GET', '/pmai/code/recent'):     lambda q, p: q.send_json(handle_code_recent(int((_qp(q, p, 'limit') or ['10'])[0]))),
            ('POST','/pmai/canon/update'):    lambda q, p: q.send_json(handle_update_canon(q.read_json())),
            ('POST','/pmai/plans/generate'):  lambda q, p: (_gen_plan(q), None),
            ('GET', '/pmai/visions'):         lambda q, p: q.send_json(handle_list_visions()),
            ('POST','/pmai/visions'):         lambda q, p: q.send_json(create_vision(q.read_json())),
            ('GET', '/pmai/roadmaps'):        lambda q, p: q.send_json({'roadmaps': list_roadmaps((_qp(q, p, 'vision_id') or [None])[0])}),
            ('POST','/pmai/roadmaps'):        lambda q, p: q.send_json(create_roadmap(q.read_json())),
            ('GET', '/pmai/plans'):           lambda q, p: q.send_json({'plans': list_plans((_qp(q, p, 'roadmap_id') or [None])[0], (_qp(q, p, 'status') or [None])[0])}),
            ('POST','/pmai/plans'):           lambda q, p: q.send_json(create_plan(q.read_json())),
            ('GET', '/pmai/principles'):      lambda q, p: q.send_json(handle_list_principles()),
            ('POST','/pmai/principles'):      lambda q, p: q.send_json(create_principle(q.read_json())),
            ('GET', '/pmai/links'):           lambda q, p: q.send_json(handle_list_links()),
            ('POST','/pmai/links'):           lambda q, p: q.send_json(create_link(q.read_json())),
            ('GET', '/pmai/tasks'):           lambda q, p: q.send_json(handle_list_tasks((_qp(q, p, 'status') or [None])[0], (_qp(q, p, 'roadmap_id') or [None])[0], (_qp(q, p, 'plan_id') or [None])[0])),
            ('POST','/pmai/tasks'):           lambda q, p: q.send_json(create_task(q.read_json())),
            ('GET', '/pmai/commits'):         lambda q, p: q.send_json({'commits': list_commits((_qp(q, p, 'status') or [None])[0], (_qp(q, p, 'task_id') or [None])[0], (_qp(q, p, 'decision_id') or [None])[0])}),
            ('POST','/pmai/commits'):         lambda q, p: q.send_json(create_commit(q.read_json())),
            ('GET', '/pmai/bugs'):            lambda q, p: q.send_json({'bugs': list_bugs((_qp(q, p, 'status') or [None])[0], (_qp(q, p, 'severity') or [None])[0], (_qp(q, p, 'commit_id') or [None])[0])}),
            ('POST','/pmai/bugs'):            lambda q, p: q.send_json(create_bug(q.read_json())),
            ('GET', '/pmai/decisions'):       lambda q, p: q.send_json(handle_list_decisions()),
            ('POST','/pmai/decisions'):       lambda q, p: q.send_json(create_decision(q.read_json())),
            ('GET', '/pmai/ideas'):           lambda q, p: q.send_json({'ideas': list_ideas((_qp(q, p, 'status') or [None])[0])}),
            ('POST','/pmai/ideas'):           lambda q, p: q.send_json(create_idea(q.read_json())),
            ('GET', '/pmai/docs'):            lambda q, p: q.send_json({'records': list_doc_records((_qp(q, p, 'status') or [None])[0], (_qp(q, p, 'layer') or [None])[0])}),
            ('GET', '/pmai/docs/content'):    lambda q, p: q.send_json(handle_get_doc_content((_qp(q, p, 'path') or [None])[0])),
            ('POST','/pmai/docs/sync'):       lambda q, p: q.send_json(handle_sync_docs()),
            ('POST','/pmai/docs/repair'):     lambda q, p: q.send_json(handle_repair_docs()),
            ('POST','/pmai/docs/prune'):      lambda q, p: q.send_json(handle_prune_docs()),
            ('PATCH','/pmai/docs'):           lambda q, p: q.send_json(handle_update_doc(q.read_json())),
            ('GET', '/pmai/docs/audit'):      lambda q, p: q.send_json(handle_audit_docs()),
            ('GET', '/pmai/daily'):           lambda q, p: q.send_json(handle_get_daily_note((_qp(q, p, 'date') or [None])[0])),
            ('GET', '/pmai/daily/history'):   lambda q, p: q.send_json(handle_list_daily_notes()),
            ('POST','/pmai/daily'):           lambda q, p: q.send_json(handle_append_daily_note(q.read_json(), (_qp(q, p, 'date') or [None])[0])),
            ('PUT', '/pmai/daily'):           lambda q, p: q.send_json(replace_daily_note(q.read_json(), (_qp(q, p, 'date') or [None])[0])),
        }
        h = _exact.get((method, path))
        if h: return lambda q, p: h(self, query)

        # -- Prefix-match routes (ordered: more-specific first) --
        id_ = None
        _prefixed = [
            ('POST','/pmai/plans/', '/advance',      lambda s, i, q: s.send_json(advance_plan(i))),
            ('POST','/pmai/tasks/',  '/notes',        lambda s, i, q: (_task_note(s, i), None)),
            ('DELETE','/pmai/task-notes/',None,       lambda s, i, q: (_delete_note(s, i), None)),
            ('GET', '/pmai/web/tasks/',  None,        lambda s, i, q: s.send_json(build_web_task_detail(i))),
            ('PATCH','/pmai/tasks/',    '/checkpoint', lambda s, i, q: (_task_checkpoint(s, i), None)),
            ('GET', '/pmai/web/commits/',None,         lambda s, i, q: s.send_json(build_web_commit_detail(i))),
            ('GET', '/pmai/web/decisions/',None,       lambda s, i, q: s.send_json(build_web_decision_detail(i))),
            ('GET', '/pmai/web/bugs/',   None,         lambda s, i, q: s.send_json(build_web_bug_detail(i))),
            ('POST','/pmai/ideas/',     '/comments',   lambda s, i, q: s.send_json(handle_create_idea_comment(i, s.read_json()))),
            ('POST','/pmai/ideas/',     '/convert',    lambda s, i, q: s.send_json(handle_convert_idea(i, s.read_json()))),
        ]
        for pm, prefix, suffix, handler in _prefixed:
            if method == pm and path.startswith(prefix) and (suffix is None or path.endswith(suffix)):
                id_ = path.split('/')[-2] if suffix else path.rsplit('/', 1)[-1]
                return lambda q, p: handler(self, id_, query)

        # -- Generic REST prefix routes (GET/PATCH single entity by id) --
        _entities = ['visions', 'roadmaps', 'plans', 'principles', 'tasks', 'commits', 'bugs', 'decisions', 'ideas']
        for ent in _entities:
            prefix = f'/pmai/{ent}/'
            if not path.startswith(prefix):
                continue
            id_ = path.rsplit('/', 1)[-1]
            if method == 'GET':
                return lambda q, p: self.send_json(
                    {'visions': get_vision, 'roadmaps': get_roadmap, 'plans': get_plan,
                     'principles': get_principle, 'tasks': get_task, 'commits': get_commit,
                     'bugs': get_bug, 'decisions': get_decision, 'ideas': get_idea}[ent](id_))
            if method == 'PATCH':
                if ent == 'visions':
                    return lambda q, p: self.send_json(update_vision(id_, self.read_json()))
                elif ent == 'roadmaps':
                    return lambda q, p: self.send_json(update_roadmap(id_, self.read_json()))
                elif ent == 'plans':
                    return lambda q, p: self.send_json(update_plan(id_, self.read_json()))
                elif ent == 'principles':
                    return lambda q, p: self.send_json(update_principle(id_, self.read_json()))
                elif ent == 'tasks':
                    return lambda q, p: (_task_update(self, id_), None)
                elif ent == 'commits':
                    return lambda q, p: self.send_json(update_commit(id_, self.read_json()))
                elif ent == 'bugs':
                    return lambda q, p: self.send_json(update_bug(id_, self.read_json()))
                elif ent == 'decisions':
                    return lambda q, p: self.send_json(update_decision_status(id_, self.read_json()['status']))
                elif ent == 'ideas':
                    return lambda q, p: (_idea_patch(self, id_), None)
            if method == 'DELETE' and ent == 'links':
                return lambda q, p: self.send_json({'ok': delete_link(id_)})

        return None


def _qp(handler, path, key):
    parsed = urlparse(handler.path)
    q = parse_qs(parsed.query)
    return q.get(key)


def _search(handler, path):
    parsed = urlparse(handler.path)
    q = parse_qs(parsed.query)
    keyword = (q.get('q') or [''])[0].lower()
    results = []
    if keyword:
        for t in list_tasks():
            if keyword in t['title'].lower():
                results.append({'type': 'task', 'id': t['id'], 'title': t['title'], 'status': t['status']})
        for c in list_commits():
            if keyword in c['title'].lower():
                results.append({'type': 'commit', 'id': c['id'], 'title': c['title'], 'status': c['status']})
        for b in list_bugs():
            if keyword in b['title'].lower() or keyword in (b.get('description') or '').lower():
                results.append({'type': 'bug', 'id': b['id'], 'title': b['title'], 'severity': b['severity']})
        for d in list_decisions():
            if keyword in d['title'].lower():
                results.append({'type': 'decision', 'id': d['id'], 'title': d['title'], 'status': d['status']})
        for i in list_ideas():
            if keyword in i['title'].lower():
                results.append({'type': 'idea', 'id': i['id'], 'title': i['title'], 'status': i['status']})
    handler.send_json({'results': results[:20]})


def _task_note(handler, task_id):
    payload = handler.read_json()
    handler.send_json(append_task_note(task_id, payload.get('content', ''), payload.get('mode', 'review')))


def _delete_note(handler, note_id):
    handler.send_json({'ok': delete_task_note(note_id)})


def _gen_plan(handler):
    payload = handler.read_json()
    from ..store import generate_plan
    handler.send_json(generate_plan(
        roadmap_id=payload.get('roadmap_id'),
        vision_id=payload.get('vision_id'),
        title=payload.get('title', ''),
        create_tasks_for_plan=bool(payload.get('create_tasks', False)),
        task_limit=int(payload.get('task_limit', 4)),
    ))


def _task_checkpoint(handler, task_id):
    payload = handler.read_json()
    handler.send_json(handle_update_checkpoint(task_id, payload.get('index'), payload.get('done', True)))


def _task_update(handler, task_id):
    payload = handler.read_json()
    handler.send_json(handle_update_task(
        task_id, payload['status'], payload.get('note', ''),
        bool(payload.get('allow_without_commit', False)),
        roadmap_id=payload.get('roadmap_id'),
        plan_id=payload.get('plan_id'),
    ))


def _idea_patch(handler, idea_id):
    payload = handler.read_json()
    if "note" in payload and "status" in payload and len(payload.keys()) <= 2:
        handler.send_json(review_idea(idea_id, payload['status'], payload.get('note', '')))
    else:
        handler.send_json(handle_update_idea(idea_id, payload))


def create_server(host: str, port: int) -> ThreadingHTTPServer:
    """Create and return a ThreadingHTTPServer instance."""
    return ThreadingHTTPServer((host, port), PMAIRequestHandler)
