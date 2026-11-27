// Reads the snapshot and the event stream. The stream is read with fetch, so the
// key travels in a header and never in a URL. Events only hint that something
// changed: the page always fetches the snapshot again, at most every 250 ms.
import { isFloor, type Floor } from './floor';
import { authHeaders } from './key';

export type SseEvent = { event: string; data: string; id: string | null };

/** An incremental parser for server-sent events. Feed it text as it arrives. */
export class SseParser {
    private buf = '';
    private event = '';
    private data: string[] = [];
    private id: string | null = null;

    feed(chunk: string): SseEvent[] {
        this.buf += chunk;
        const out: SseEvent[] = [];
        let nl: number;
        while ((nl = this.buf.search(/\r\n|\n|\r/)) !== -1) {
            const m = /^(\r\n|\n|\r)/.exec(this.buf.slice(nl));
            const len = m ? m[0].length : 1;
            // A bare CR at the very end may be half of a CRLF: wait for more.
            if (this.buf[nl] === '\r' && nl + 1 === this.buf.length) break;
            const line = this.buf.slice(0, nl);
            this.buf = this.buf.slice(nl + len);
            if (line === '') {
                if (this.data.length > 0 || this.event !== '') out.push({ event: this.event || 'message', data: this.data.join('\n'), id: this.id });
                this.event = '';
                this.data = [];
                continue;
            }
            if (line.startsWith(':')) continue;
            const c = line.indexOf(':');
            const field = c === -1 ? line : line.slice(0, c);
            let value = c === -1 ? '' : line.slice(c + 1);
            if (value.startsWith(' ')) value = value.slice(1);
            if (field === 'event') this.event = value;
            else if (field === 'data') this.data.push(value);
            else if (field === 'id') this.id = value;
        }
        return out;
    }
}

export type ClientOptions = {
    key: string;
    fetch: typeof fetch;
    onSnapshot: (f: Floor) => void;
    /** 'denied' for a 401 (the key is not accepted), 'failed' for anything else. */
    onFailure: (kind: 'denied' | 'failed') => void;
    minGapMs?: number;
};

export type Client = { stop(): void; refresh(): void };

const BACKOFF_START = 1000;
const BACKOFF_MAX = 30000;

export function startClient(o: ClientOptions): Client {
    const gap = o.minGapMs ?? 250;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | null = null;
    let lastRun = -Infinity;
    let inflight = false;
    let again = false;
    let abort: AbortController | null = null;
    let lastId: string | null = null;

    const stop = () => {
        stopped = true;
        if (timer) clearTimeout(timer);
        timer = null;
        abort?.abort();
    };

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
                o.onFailure('failed');
                return;
            }
            const body: unknown = await r.json();
            if (stopped) return;
            if (!isFloor(body)) {
                o.onFailure('failed');
                return;
            }
            o.onSnapshot(body);
        } catch {
            if (!stopped) o.onFailure('failed');
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
                const reader = r.body.getReader();
                const dec = new TextDecoder();
                const p = new SseParser();
                for (;;) {
                    const { value, done } = await reader.read();
                    if (done) break;
                    for (const ev of p.feed(dec.decode(value, { stream: true }))) {
                        if (ev.event === 'hello') {
                            backoff = BACKOFF_START;
                            refresh();
                        } else if (ev.event === 'journal' || ev.event === 'floor' || ev.event === 'reset') {
                            if (ev.event === 'journal' && ev.id !== null && /^[0-9]{1,15}$/.test(ev.id)) lastId = ev.id;
                            if (ev.event === 'reset') lastId = null;
                            refresh();
                        }
                    }
                }
            } catch {
                if (stopped) return;
                // The stream is down. Read the snapshot anyway, so a failure shows and a recovery is seen.
                refresh();
            }
            if (stopped) return;
            await new Promise<void>((res) => setTimeout(res, backoff));
            backoff = Math.min(backoff * 2, BACKOFF_MAX);
        }
    }

    refresh();
    void stream();
    return { stop, refresh };
}
