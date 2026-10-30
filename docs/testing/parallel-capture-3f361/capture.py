"""One-shot local observation. No business POST, database, model or storage APIs.
Attach before A, reload manually, then arm A/B immediately before UI sends.
Only this process's explicit probes are GETs. Captures are sanitized JSONL.
"""
import argparse
import asyncio
import json
import re
import time
from pathlib import Path
from urllib.parse import urlsplit, quote


def decode(value):
    if isinstance(value, (bytes, bytearray)):
        value = value.decode('utf-8', 'replace')
    try:
        return json.loads(value) if isinstance(value, str) else value
    except (ValueError, TypeError):
        return value


def objects(value):
    value = decode(value)
    if isinstance(value, dict):
        yield value
        for v in value.values():
            yield from objects(v)
    elif isinstance(value, list):
        for v in value:
            yield from objects(v)


class Recorder:
    def __init__(self, page, api_root, bot, contact, output, timeout=10):
        self.page, self.root = page, api_root.rstrip('/')
        self.bot, self.contact, self.timeout = bot, contact, timeout
        self.prefix = f'/bots/{bot}/sessions/{contact}'
        self.out = Path(output).open('x', encoding='utf-8')
        self.seq, self.request_counter = 0, 0
        self.pending, self.requests, self.roles, self.delegations = set(), {}, {}, {}
        self.armed, self.ws_seen, self.auth_seen = None, False, False
        self.headers, self.secrets, self.blocked = {}, {self.root, urlsplit(self.root).netloc}, set()
        self.frames, self.sockets, self.worker_anchors = [], [], {}
        self.closed = False
        self.wake = asyncio.Event()
        self.stopping = False
        page.on('request', self.on_request)
        page.on('response', self.on_response)
        page.on('requestfailed', self.on_failed)
        page.on('websocket', self.on_socket)
        self.emit('recorder_started', bot=bot, contact=contact,
                  status='RELOAD_BEFORE_ARM; observer does not send business requests')

    def safe(self, value):
        if isinstance(value, dict):
            return {k: ('[REDACTED]' if re.search(
                r'authorization|cookie|token|password|secret|api[_-]?key', k, re.I)
                else self.safe(v)) for k, v in value.items()}
        if isinstance(value, (list, tuple)):
            return [self.safe(v) for v in value]
        if isinstance(value, str):
            for s in sorted(self.secrets, key=len, reverse=True):
                if s:
                    value = value.replace(s, '[REDACTED]')
            value = re.sub(r'(?i)(?:https?|wss?)://[^\s"<>]+', '[ADDRESS]', value)
            value = re.sub(r'(?i)Bearer\s+\S+', '[AUTH]', value)
            value = re.sub(r'(?<![\w.])(?:\d{1,3}\.){3}\d{1,3}(?::\d+)?', '[IP]', value)
            return value
        return value

    def emit(self, kind, **data):
        if self.closed:
            return None
        self.seq += 1
        row = dict(seq=self.seq, monotonic_ns=time.monotonic_ns(), kind=kind, **data)
        self.out.write(json.dumps(self.safe(row), ensure_ascii=False) + '\n')
        self.out.flush()
        return self.seq

    def spawn(self, coro):
        task = asyncio.create_task(coro)
        self.pending.add(task)
        def done(t):
            self.pending.discard(t)
            if not t.cancelled() and t.exception():
                self.emit('callback_error', error_type=type(t.exception()).__name__)
        task.add_done_callback(done)

    def relative(self, url):
        # Exact configured origin/root, not a substring/guessed /api prefix.
        if not url.startswith(self.root + '/'):
            return None
        path = urlsplit(url[len(self.root):]).path
        if path == f'/bots/{self.bot}/messages':
            from urllib.parse import parse_qs
            if parse_qs(urlsplit(url).query).get('session_id') == [self.contact]:
                return path
        if path == self.prefix or path.startswith(self.prefix + '/'):
            return path
        return None

    def arm(self, role):
        if role not in ('A', 'B') or role in self.roles or self.armed:
            raise ValueError('role already assigned or another arm is pending')
        if not self.ws_seen or not self.auth_seen:
            raise ValueError('reload first: fresh source socket and authenticated API request required')
        self.armed = role
        self.emit('armed', role=role)

    def bind(self, invocation, transport, request_id=None):
        if not self.armed or not invocation:
            return
        role, self.armed = self.armed, None
        self.roles[role] = dict(request_invocation=invocation, transport=transport,
                                request_id=request_id, admitted_invocation=invocation if transport == 'ws' else None)
        self.emit('role_request', role=role, **self.roles[role])

    def on_request(self, req):
        path = self.relative(req.url)
        if path is None:
            return
        # Credentials only in memory; no headers, root, cookies or URL queries logged.
        auth = req.headers.get('authorization')
        if auth:
            self.headers = {'Authorization': auth}
            self.secrets.update((auth, auth.removeprefix('Bearer ')))
        self.request_counter += 1
        key = self.request_counter
        body = decode(req.post_data)
        entry = dict(key=key, path=path, method=req.method, body=body, done=False)
        self.requests[req] = entry
        self.emit('http_request', request_id=key, method=req.method, path=path, body=body)
        if req.method == 'POST' and path == self.prefix + '/follow-up-queue' and isinstance(body, dict):
            self.bind(body.get('invocation_id'), 'queue', key)

    def on_response(self, response):
        if response.request in self.requests:
            # Retain task until the asynchronous full body has been consumed.
            self.spawn(self.consume(response))

    async def consume(self, response):
        entry = self.requests[response.request]
        try:
            body = decode(await asyncio.wait_for(response.body(), self.timeout))
            entry['done'] = True
            if response.status == 200 and entry['method'] == 'GET':
                self.auth_seen = True
            seq = self.emit('http_response', request_id=entry['key'], status=response.status, body=body)
            for role, r in self.roles.items():
                if r['transport'] != 'queue' or r['request_id'] != entry['key']:
                    continue
                if response.status == 202 and isinstance(body, dict) and body.get('item_id'):
                    r['item_id'] = body['item_id']
                    r['admitted_invocation'] = 'follow-up:' + body['item_id']
                    r['enqueued_during_run_id'] = body.get('enqueued_during_run_id')
                    self.emit('queue_mapping', role=role, response_seq=seq, **r)
                    self.wake.set()
                else:
                    self.emit('queue_unresolved', role=role, response_seq=seq,
                              reason='no accepted item identity; never resend')
        except (Exception, asyncio.CancelledError) as e:
            entry['done'] = True
            self.emit('http_body_unknown', request_id=entry['key'], error_type=type(e).__name__)

    def on_failed(self, req):
        if req in self.requests:
            self.requests[req]['done'] = True
            self.emit('http_failed_unknown', request_id=self.requests[req]['key'], reason='transport failure; no business retry')

    def on_socket(self, ws):
        # WebSocket URL may contain auth query; never retain or print it.
        path = urlsplit(ws.url).path
        expected = urlsplit(self.root).path.rstrip('/') + f'/bots/{self.bot}/web/ws'
        if path != expected or urlsplit(ws.url).netloc != urlsplit(self.root).netloc:
            return
        self.ws_seen = True
        self.emit('socket_open', bot=self.bot)
        handlers = {
            'framesent': lambda p: self.frame('sent', p),
            'framereceived': lambda p: self.frame('received', p),
            'socketerror': lambda _: self.emit('socket_error_unknown'),
            'close': lambda: self.emit('socket_closed'),
        }
        self.sockets.append((ws, handlers))
        for event, handler in handlers.items():
            ws.on(event, handler)

    def frame(self, direction, payload):
        body = decode(payload)
        seq = self.emit('ws_' + direction, body=body)
        if direction == 'sent' and isinstance(body, dict) and body.get('type') == 'message' and body.get('session_id') == self.contact:
            self.bind(body.get('invocation_id'), 'ws')
        self.wake.set()
        if direction == 'received':
            # Preserve frames without invocation_id; correlate by original admitted run.
            self.frames.append((body, seq))
            self.correlate()

    def correlate(self):
        a = self.roles.get('A', {})
        for body, seq in self.frames:
            for obj in objects(body):
                if (a.get('admitted_invocation') and obj.get('invocation_id') == a['admitted_invocation']) or (a.get('run_id') and obj.get('run_id') == a['run_id']):
                    self.discover(obj, seq)

    def discover(self, body, seq):
        for obj in objects(body):
            if obj.get('delegation_id') and obj.get('source_session_id') == self.contact and obj.get('target_bot_id'):
                did = obj['delegation_id']
                if did not in self.delegations:
                    self.emit('delegation_observed', delegation_id=did, evidence_seq=seq)
                self.delegations[did] = obj

    async def get(self, path):
        if not self.auth_seen or path in self.blocked:
            return None, None
        start = self.emit('probe_start', path=path)
        try:
            result = await self.page.evaluate('''async ({url,headers,timeout}) => {
              const controller = new AbortController();
              const timer = setTimeout(() => controller.abort(), timeout);
              try {
                const r = await fetch(url, {method:'GET',headers,credentials:'include',
                  cache:'no-store',redirect:'error',signal:controller.signal});
                const text = await r.text(); let body;
                try { body=JSON.parse(text); } catch { body=text; }
                return {status:r.status,body};
              } catch { return {status:null,body:null,error:'transport_unknown'}; }
              finally { clearTimeout(timer); }
            }''', dict(url=self.root + path, headers=self.headers, timeout=self.timeout*1000))
        except asyncio.CancelledError:
            self.emit('probe_cancelled_unknown', start_seq=start, path=path)
            raise
        except Exception as e:
            result = dict(status=None, body=None, error=type(e).__name__)
        end = self.emit('probe_end', start_seq=start, path=path, **result)
        if result['status'] != 200:
            # Fail closed: no auth/transport retry loop. Explicit read-back can be reviewed later.
            self.blocked.add(path)
            self.emit('probe_blocked', path=path, reason='non-200 or unknown; do not resubmit business input')
            return None, end
        return result['body'], end

    async def sample(self):
        # Sequential awaited GETs prove causal order, not cross-machine wall-clock arithmetic.
        b = None
        b_seq = None
        for role, r in list(self.roles.items()):
            inv = r.get('admitted_invocation')
            if not inv:
                continue
            v, seq = await self.get(self.prefix + '/invocations/' + quote(inv, safe=''))
            self.emit('run_binding', role=role, evidence_seq=seq, observation=v)
            if isinstance(v, dict) and v.get('found') and (v.get('session_id') != self.contact or v.get('invocation_id') != inv or not v.get('run_id') or not v.get('turn_id')):
                self.emit('invocation_identity_mismatch', role=role, evidence_seq=seq)
                continue
            if isinstance(v, dict) and v.get('found'):
                r['run_id'], r['turn_id'] = v.get('run_id'), v.get('turn_id')
                if role == 'A':
                    self.correlate()
                    history, hs = await self.get(f'/bots/{self.bot}/messages?session_id={quote(self.contact)}&limit=100')
                    if isinstance(history, dict):
                        for turn in history.get('items', []):
                            if turn.get('turn_id') == v.get('turn_id'):
                                self.discover(turn, hs)
            if role == 'B' and isinstance(v, dict) and v.get('found') and v.get('state') == 'completed':
                b, b_seq = v, seq
        history_seq, turn = None, None
        if b:
            history, history_seq = await self.get(f'/bots/{self.bot}/messages?session_id={quote(self.contact)}&limit=100')
            if isinstance(history, dict):
                turn = [t for t in history.get('items', []) if t.get('turn_id') == b.get('turn_id')]
        for did in list(self.delegations):
            d, seq = await self.get(self.prefix + '/delegations/' + quote(did, safe=''))
            if isinstance(d, dict) and d.get('target_run_id'):
                anchor = (d.get('target_bot_id'), d.get('target_session_id'), d['target_run_id'])
                original = self.delegations[did]
                if d.get('delegation_id') != did or d.get('source_session_id') != self.contact or d.get('target_bot_id') != original['target_bot_id'] or not d.get('target_session_id') or (did in self.worker_anchors and self.worker_anchors[did] != anchor) or (original.get('target_run_id') and original['target_run_id'] != anchor[2]):
                    self.emit('worker_identity_mismatch', delegation_id=did, evidence_seq=seq)
                    continue
                self.worker_anchors[did] = anchor
            if b and turn and isinstance(d, dict) and d.get('delegation_id') == did and d.get('target_run_id') and d.get('target_run_state') == 'running':
                self.emit('overlap_candidate', b_run=b.get('run_id'), b_turn=b.get('turn_id'),
                          b_completed_seq=b_seq, persisted_history_seq=history_seq,
                          subsequent_worker_seq=seq, delegation_id=did,
                          worker_run=d['target_run_id'], requires_substantive_final_text_and_ui_review=True)
        if b and not turn:
            self.emit('history_window_missing', b_turn=b.get('turn_id'), reason='no text/latest-run fallback')

    async def close(self):
        self.stopping = True
        # Include requests whose response event has not arrived yet, not only body tasks.
        deadline = time.monotonic() + self.timeout
        while any(not e['done'] for e in self.requests.values()) and time.monotonic() < deadline:
            await asyncio.sleep(0.02)
        self.page.remove_listener('request', self.on_request)
        self.page.remove_listener('response', self.on_response)
        self.page.remove_listener('requestfailed', self.on_failed)
        self.page.remove_listener('websocket', self.on_socket)
        for ws, handlers in self.sockets:
            for event, handler in handlers.items():
                ws.remove_listener(event, handler)
        # All already scheduled response bodies must finish or explicitly become unknown.
        while self.pending:
            tasks = list(self.pending)
            _, remaining = await asyncio.wait(tasks, timeout=self.timeout)
            for task in remaining:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
        for entry in self.requests.values():
            if not entry['done']:
                self.emit('http_response_unknown_on_stop', request_id=entry['key'])
        gaps = [role + '_request_missing' for role in ('A', 'B') if role not in self.roles]
        gaps += [role + '_queue_identity_unknown' for role, r in self.roles.items()
                 if r['transport'] == 'queue' and not r.get('admitted_invocation')]
        if not self.delegations:
            gaps.append('A_delegation_identity_not_observed')
        self.emit('stopped', roles=self.roles, delegation_ids=list(self.delegations),
                  unresolved_arm=self.armed, collection_gaps=gaps,
                  status='capture_only_not_automatic_acceptance')
        self.closed = True
        self.out.close()


async def run(args):
    from playwright.async_api import async_playwright
    async with async_playwright() as p:
        browser = await p.chromium.connect_over_cdp(args.cdp)
        pages = [page for c in browser.contexts for page in c.pages]
        if args.page_index >= len(pages):
            raise ValueError('page index unavailable')
        rec = Recorder(pages[args.page_index], args.api_root, args.bot, args.contact, args.output, args.timeout)
        async def watcher():
            while not rec.stopping:
                rec.wake.clear()
                try:
                    await rec.sample()
                except Exception as e:
                    rec.emit('observer_failed', error_type=type(e).__name__)
                    return
                try:
                    await asyncio.wait_for(rec.wake.wait(), args.interval)
                except asyncio.TimeoutError:
                    pass  # observation cadence, never a pass criterion
        watch = asyncio.create_task(watcher())
        try:
            print('LISTENING. Manually reload selected page before A. Commands: arm A, arm B, sample, stop.', flush=True)
            while True:
                command = (await asyncio.to_thread(input, '> ')).strip()
                try:
                    if command in ('arm A', 'arm B'):
                        rec.arm(command[-1]); print('ARMED; now send exactly once using normal UI.', flush=True)
                    elif command == 'sample':
                        rec.wake.set()
                        print('GET sample requested; no business action sent.', flush=True)
                    elif command == 'stop':
                        break
                except ValueError as e:
                    print(str(e), flush=True)
        finally:
            rec.stopping = True
            watch.cancel()
            await asyncio.gather(watch, return_exceptions=True)
            await rec.close()
        # Disconnect Playwright only; never close the user's browser/page.


if __name__ == '__main__':
    ap = argparse.ArgumentParser()
    ap.add_argument('--cdp', required=True, help='existing local browser CDP endpoint; not written to evidence')
    ap.add_argument('--page-index', type=int, required=True)
    ap.add_argument('--api-root', required=True, help='existing authorized API root; never persisted')
    ap.add_argument('--bot', required=True)
    ap.add_argument('--contact', required=True)
    ap.add_argument('--output', required=True, help='new restricted local JSONL path')
    ap.add_argument('--interval', type=float, default=1, help='GET observation cadence only')
    ap.add_argument('--timeout', type=float, default=10, help='body/probe timeout, preserved as unknown')
    a = ap.parse_args()
    if a.interval <= 0 or a.timeout <= 0:
        ap.error('interval and timeout must be positive')
    try:
        asyncio.run(run(a))
    except Exception as e:
        # Playwright errors can contain URLs or headers; never dump raw exception strings.
        print('Recorder stopped:', type(e).__name__, '(inspect preserved evidence; do not resend)')
        raise SystemExit(1)
