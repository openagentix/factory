// Reads the snapshot and the event stream. The stream is read with fetch, so the
// key travels in a header and never in a URL. Events only hint that something
// changed: the page always fetches the snapshot again, at most every 250 ms.
import type { Floor } from './floor';
import { parseFloor } from './parse';
import { authHeaders } from './key';

const MAX_LINE = 64 * 1024;
const MAX_BUFFER = 1024 * 1024;
export class SseOverflow extends Error {}

export type SseEvent = { event: string; data: string; id: string | null };

/** An incremental parser for server-sent events. Feed it text as it arrives. */
export class SseParser {
    private buf = '';
    private event = '';
    private data: string[] = [];
    private id: string | null = null;
    private held = 0;

    /** Throws when a line is over 64 KiB, more than 1 MiB is buffered, or more than 1 MiB of data waits for an event. */
    feed(chunk: string): SseEvent[] {
        this.buf += chunk;
        if (this.buf.length > MAX_BUFFER) throw new SseOverflow();
        const out: SseEvent[] = [];
        let nl: number;
        while ((nl = this.buf.search(/\r\n|\n|\r/)) !== -1) {
            const m = /^(\r\n|\n|\r)/.exec(this.buf.slice(nl));
            const len = m ? m[0].length : 1;
            // A bare CR at the very end may be half of a CRLF: wait for more.
            if (this.buf[nl] === '\r' && nl + 1 === this.buf.length) break;
            if (nl > MAX_LINE) throw new SseOverflow();
            const line = this.buf.slice(0, nl);
            this.buf = this.buf.slice(nl + len);
            if (line === '') {
                if (this.data.length > 0 || this.event !== '') out.push({ event: this.event || 'message', data: this.data.join('\n'), id: this.id });
                this.event = '';
                this.data = [];
                this.held = 0;
                continue;
            }
            if (line.startsWith(':')) continue;
            const c = line.indexOf(':');
            const field = c === -1 ? line : line.slice(0, c);
            let value = c === -1 ? '' : line.slice(c + 1);
            if (value.startsWith(' ')) value = value.slice(1);
            if (field === 'event') this.event = value;
            else if (field === 'data') {
                this.held += value.length;
                if (this.held > MAX_BUFFER) throw new SseOverflow();
                this.data.push(value);
            }
            else if (field === 'id') this.id = value;
        }
        if (this.buf.length > MAX_LINE) throw new SseOverflow();
        return out;
    }
}

export type ClientOptions = {
    key: string;
    fetch: typeof fetch;
    onSnapshot: (f: Floor) => void;
    /** 'denied' for a 401 (the key is not accepted), 'failed' for anything else. */
    onFailure: (kind: 'denied' | 'failed') => void;
    random?: () => number;
    minGapMs?: number;
};

/** How a write ended. A 200 or a 202 is "ok"; anything else carries the server's fixed code. Status 0 means no answer. */
export type WriteResult = { ok: true; status: number; result: 'done' | 'pending' | 'running' } | { ok: false; status: number; code: string };

/** How a read ended: the parsed body of a 200, or the server's fixed code. Status 0 means no answer. */
export type ReadResult = { ok: true; body: unknown } | { ok: false; status: number; code: string };

export type Client = {
    stop(): void;
    refresh(): void;
    write(path: string, body: unknown, idemKey: string): Promise<WriteResult>;
    read(path: string): Promise<ReadResult>;
};

const BACKOFF_START = 1000;
const BACKOFF_MAX = 30000;
// A stream must stay up this long before the back-off starts again from the beginning.
const STABLE_MS = 30000;

export function startClient(o: ClientOptions): Client {
    const gap = o.minGapMs ?? 250;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let lastRun = -Infinity;
    let inflight = false;
    let again = false;
    let abort: AbortController | null = null;
    let lastId: string | null = null;
    let retryN = 0;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;
    const rnd = o.random ?? Math.random;
    const delay = (n: number) => Math.min(BACKOFF_START * 2 ** n, BACKOFF_MAX) * (1 + rnd() * 0.2);

    const stop = () => {
        stopped = true;
        if (timer) clearTimeout(timer);
        if (retryTimer) clearTimeout(retryTimer);
        timer = null;
        retryTimer = null;
        abort?.abort();
    };

    // A failed or rejected snapshot is reported once and read again after a growing delay, until a good one arrives.
    function failedRead(): void {
        o.onFailure('failed');
        if (stopped || retryTimer) return;
        retryTimer = setTimeout(() => {
            retryTimer = null;
            refresh();
        }, delay(retryN));
        retryN++;
    }

    async function snapshot(): Promise<void> {
        if (inflight) {
            again = true;
            return;
        }
        inflight = true;
        try {
            const r = await o.fetch('/api/v1/floor', { headers: authHeaders(o.key), cache: 'no-store', credentials: 'omit' });
            if (stopped) return;
            if (r.status === 401) {
                stop();
                o.onFailure('denied');
                return;
            }
            if (!r.ok) {
                failedRead();
                return;
            }
            const floor = parseFloor(await r.json());
            if (stopped) return;
            if (floor === null) {
                failedRead();
                return;
            }
            retryN = 0;
            o.onSnapshot(floor);
        } catch {
            if (!stopped) failedRead();
        } finally {
            inflight = false;
            if (again && !stopped) {
                again = false;
                refresh();
            }
        }
    }

    function refresh(): void {
        if (stopped || timer) return;
        const wait = Math.max(0, lastRun + gap - Date.now());
        timer = setTimeout(() => {
            timer = null;
            lastRun = Date.now();
            void snapshot();
        }, wait);
    }

    async function stream(): Promise<void> {
        let backoff = BACKOFF_START;
        while (!stopped) {
            let upSince = 0;
            try {
                abort = new AbortController();
                const headers: Record<string, string> = { ...authHeaders(o.key), Accept: 'text/event-stream' };
                if (lastId !== null) headers['Last-Event-ID'] = lastId;
                const r = await o.fetch('/api/v1/events', { headers, cache: 'no-store', credentials: 'omit', signal: abort.signal });
                if (r.status === 401) {
                    stop();
                    o.onFailure('denied');
                    return;
                }
                if (!r.ok || !r.body) throw new Error('no stream');
                upSince = Date.now();
                const reader = r.body.getReader();
                const dec = new TextDecoder();
                const p = new SseParser();
                for (;;) {
                    const { value, done } = await reader.read();
                    if (done) break;
                    for (const ev of p.feed(dec.decode(value, { stream: true }))) {
                        if (ev.event === 'hello') {
                            refresh();
                        } else if (ev.event === 'journal' || ev.event === 'floor' || ev.event === 'reset') {
                            if (ev.event === 'journal' && ev.id !== null && /^[0-9]{1,15}$/.test(ev.id)) lastId = ev.id;
                            if (ev.event === 'reset') lastId = null;
                            refresh();
                        }
                    }
                }
            } catch {
                abort?.abort();
                if (stopped) return;
                // The stream is down. Read the snapshot anyway, so a failure shows and a recovery is seen.
                refresh();
            }
            if (stopped) return;
            if (upSince > 0 && Date.now() - upSince >= STABLE_MS) backoff = BACKOFF_START;
            await new Promise<void>((res) => setTimeout(res, backoff * (1 + rnd() * 0.2)));
            backoff = Math.min(backoff * 2, BACKOFF_MAX);
        }
    }

    // A write is one POST. The page never trusts the reply to change what it draws: it only reads the snapshot again.
    async function write(path: string, body: unknown, idemKey: string): Promise<WriteResult> {
        let out: WriteResult = { ok: false, status: 0, code: 'unreachable' };
        try {
            const r = await o.fetch(path, {
                method: 'POST',
                headers: { ...authHeaders(o.key), 'Content-Type': 'application/json', 'Idempotency-Key': idemKey },
                body: JSON.stringify(body),
                credentials: 'omit',
                cache: 'no-store',
                redirect: 'error',
            });
            let j: unknown = null;
            try {
                j = await r.json();
            } catch {
                /* a reply with no body */
            }
            const rec = j !== null && typeof j === 'object' ? (j as Record<string, unknown>) : {};
            if (r.status === 401) {
                stop();
                o.onFailure('denied');
                return { ok: false, status: 401, code: 'unauthorized' };
            }
            if (r.status === 200 || r.status === 202) {
                const res = rec.result;
                out =
                    (r.status === 200 && (res === 'done' || res === 'pending')) || (r.status === 202 && res === 'running')
                        ? { ok: true, status: r.status, result: res }
                        : { ok: false, status: r.status, code: 'internal' };
            } else {
                const code = typeof rec.error === 'string' && /^[a-z_]{1,40}$/.test(rec.error) ? rec.error : 'internal';
                out = { ok: false, status: r.status, code };
            }
        } catch {
            /* no answer: the write may or may not have happened, and the snapshot will say */
        }
        refresh();
        return out;
    }

    // A read of one thing, like a piece or an undo preview. It changes nothing on the screen by itself.
    async function read(path: string): Promise<ReadResult> {
        try {
            const r = await o.fetch(path, { headers: authHeaders(o.key), cache: 'no-store', credentials: 'omit', redirect: 'error' });
            if (stopped) return { ok: false, status: 0, code: 'unreachable' };
            if (r.status === 401) {
                stop();
                o.onFailure('denied');
                return { ok: false, status: 401, code: 'unauthorized' };
            }
            let j: unknown = null;
            try {
                j = await r.json();
            } catch {
                /* a reply with no body */
            }
            if (r.status === 200) return { ok: true, body: j };
            const rec = j !== null && typeof j === 'object' ? (j as Record<string, unknown>) : {};
            const code = typeof rec.error === 'string' && /^[a-z_]{1,40}$/.test(rec.error) ? rec.error : 'internal';
            return { ok: false, status: r.status, code };
        } catch {
            return { ok: false, status: 0, code: 'unreachable' };
        }
    }

    refresh();
    void stream();
    return { stop, refresh, write, read };
}
