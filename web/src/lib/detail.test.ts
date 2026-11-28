// The server's reply for a piece and for an undo preview is untrusted. One bad field rejects the whole reply.
import { describe, expect, it, vi } from 'vitest';
import { parseDetail, parseUndoPreview } from './detail';
import { startClient } from './client';
import { FIXTURES, KEY, clone } from './testkit';

type Any = any;
const C40 = 'c'.repeat(40);
const good = (): Any => ({
    schemaVersion: 1, mode: 'demo', generatedAt: '2026-11-27T12:00:00Z',
    piece: clone((FIXTURES.demo as Any).pieces.find((p: Any) => p.id === 't46')),
    route: [{ station: 3, at: '2026-11-27T10:00:00Z' }, { station: 6, at: null }],
    rulings: [{ text: 'x', truncated: false }], unconfirmed: [],
    undo: { change: 'run-46', stagingSeq: 7, commit: C40 },
});
const prev = (): Any => ({
    mode: 'demo', change: 'run-46', stagingSeq: 7, commit: C40, alsoRemoved: ['run-47'],
    tickets: [{ change: 'run-46', ticket: 46 }, { change: 'run-47', ticket: 47 }],
});

describe('parseDetail', () => {
    it('takes a whole reply, with or without undo', () => {
        expect(parseDetail(good())).not.toBeNull();
        expect(parseDetail({ ...good(), undo: null })).not.toBeNull();
    });
    it('refuses anything that is not whole', () => {
        const bads: ((d: Any) => void)[] = [
            (d) => delete d.undo,
            (d) => (d.schemaVersion = 2),
            (d) => (d.mode = 'x'),
            (d) => (d.generatedAt = 'now'),
            (d) => (d.piece.station = 8),
            (d) => (d.piece.id = '<b>'),
            (d) => (d.route = {}),
            (d) => d.route.push({ station: 3, at: null }),
            (d) => (d.route[0].at = 'yesterday'),
            (d) => (d.rulings = [{ text: 5, truncated: false }]),
            (d) => (d.unconfirmed = [{ text: 'x' }]),
            (d) => (d.rulings = Array.from({ length: 21 }, () => ({ text: 'x', truncated: false }))),
            (d) => (d.undo.change = 'run 46'),
            (d) => (d.undo.change = 'run.46'),
            (d) => (d.undo.stagingSeq = 0),
            (d) => (d.undo.commit = 'abc'),
            (d) => (d.undo.commit = C40.toUpperCase()),
        ];
        bads.forEach((f, i) => {
            const d = good();
            f(d);
            expect(parseDetail(d), `case ${i}`).toBeNull();
        });
        expect(parseDetail(null)).toBeNull();
        expect(parseDetail([])).toBeNull();
        expect(parseDetail('x')).toBeNull();
    });
});

describe('parseUndoPreview', () => {
    it('takes a whole preview', () => {
        expect(parseUndoPreview(prev())).not.toBeNull();
        expect(parseUndoPreview({ ...prev(), alsoRemoved: [], tickets: [{ change: 'run-46', ticket: 46 }] })).not.toBeNull();
    });
    it('refuses a list that does not match, in any way', () => {
        const bads: ((d: Any) => void)[] = [
            (d) => (d.alsoRemoved = null),
            (d) => (d.alsoRemoved = ['run-47', 'run-47']),
            (d) => (d.alsoRemoved = ['run-46']),
            (d) => (d.tickets = d.tickets.slice(0, 1)),
            (d) => d.tickets.reverse(),
            (d) => (d.tickets[1].ticket = 0),
            (d) => (d.tickets[1].ticket = '47'),
            (d) => (d.tickets[1].change = 'run-48'),
            (d) => (d.change = 'run/46'),
            (d) => (d.stagingSeq = -1),
            (d) => (d.commit = 'zz'),
            (d) => (d.alsoRemoved = Array.from({ length: 51 }, (_, i) => 'run-' + (100 + i))),
        ];
        bads.forEach((f, i) => {
            const d = prev();
            f(d);
            expect(parseUndoPreview(d), `case ${i}`).toBeNull();
        });
    });
});

describe('client.read', () => {
    const mk = (respond: () => Response | Promise<Response>) => {
        const urls: { url: string; init?: RequestInit }[] = [];
        const denied = vi.fn();
        const fetch = (async (url: string, init?: RequestInit) => {
            urls.push({ url, init });
            if (String(url).endsWith('/floor') || String(url).endsWith('/events')) return new Response('{}', { status: 500 });
            return respond();
        }) as unknown as typeof globalThis.fetch;
        const c = startClient({ key: KEY, fetch, onSnapshot: () => {}, onFailure: denied, minGapMs: 100000 });
        return { c, urls, denied };
    };
    it('sends the key in a header, never in the address, and gives the body of a 200', async () => {
        const { c, urls } = mk(() => new Response('{"a":1}', { status: 200 }));
        const r = await c.read('/api/v1/pieces/t5');
        c.stop();
        expect(r).toEqual({ ok: true, body: { a: 1 } });
        const u = urls.find((x) => x.url === '/api/v1/pieces/t5')!;
        expect((u.init!.headers as Record<string, string>).Authorization).toBe('Bearer ' + KEY);
        expect(u.url).not.toContain(KEY);
        expect(u.init!.credentials).toBe('omit');
    });
    it('gives the fixed code of a refusal, a code it does not know as internal, and no answer as status 0', async () => {
        let n = 0;
        const { c } = mk(() => {
            n++;
            if (n === 1) return new Response('{"error":"stale"}', { status: 409 });
            if (n === 2) return new Response('{"error":"<img>"}', { status: 418 });
            return Promise.reject(new TypeError('network'));
        });
        expect(await c.read('/x')).toEqual({ ok: false, status: 409, code: 'stale' });
        expect(await c.read('/x')).toEqual({ ok: false, status: 418, code: 'internal' });
        expect(await c.read('/x')).toEqual({ ok: false, status: 0, code: 'unreachable' });
        c.stop();
    });
    it('a 401 is a denied key', async () => {
        const { c, denied } = mk(() => new Response('{}', { status: 401 }));
        const r = await c.read('/x');
        expect(r.ok).toBe(false);
        expect(denied).toHaveBeenCalledWith('denied');
    });
});
