import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import demo from '../fixtures/floor.demo.json';
import { SseParser, startClient } from './client';
import type { Floor } from './floor';

const KEY = 'abcDEF0123456789_-abcDEF0123456789_-xyzXYZ';

describe('SseParser', () => {
    it('reads events split across chunks, comments and CRLF', () => {
        const p = new SseParser();
        expect(p.feed('event: hel')).toEqual([]);
        expect(p.feed('lo\ndata: {"a":1}\n\n: ping\n\n')).toEqual([{ event: 'hello', data: '{"a":1}', id: null }]);
        expect(p.feed('id: 7\r\nevent: journal\r\ndata: x\r\n\r\n')).toEqual([{ event: 'journal', data: 'x', id: '7' }]);
    });
});

function streamOf(text: string[]): Response {
    const enc = new TextEncoder();
    let i = 0;
    const body = new ReadableStream<Uint8Array>({
        pull(c) {
            if (i < text.length) c.enqueue(enc.encode(text[i++]));
            else c.close();
        },
    });
    return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

describe('the client', () => {
    beforeEach(() => vi.useFakeTimers());
    afterEach(() => vi.useRealTimers());

    it('sends the key only as a header, never in the URL, and passes the snapshot on', async () => {
        const calls: { url: string; init?: RequestInit }[] = [];
        const f = (async (url: string, init?: RequestInit) => {
            calls.push({ url: String(url), init });
            if (String(url).endsWith('/floor')) return new Response(JSON.stringify(demo), { status: 200 });
            return streamOf(['event: hello\ndata: {}\n\n']);
        }) as unknown as typeof fetch;
        const got: Floor[] = [];
        const c = startClient({ key: KEY, fetch: f, onSnapshot: (x) => got.push(x), onFailure: () => {} });
        await vi.advanceTimersByTimeAsync(300);
        c.stop();
        expect(got.length).toBeGreaterThanOrEqual(1);
        expect(calls.map((x) => x.url)).toContain('/api/v1/floor');
        expect(calls.map((x) => x.url)).toContain('/api/v1/events');
        for (const x of calls) {
            expect(x.url).not.toContain(KEY);
            expect(x.url).not.toContain('?');
            expect((x.init?.headers as Record<string, string>).Authorization).toBe('Bearer ' + KEY);
            expect(x.init?.credentials).toBe('omit');
        }
    });

    it('fetches the snapshot at most every 250 ms however many events arrive', async () => {
        let floors = 0;
        const f = (async (url: string) => {
            if (String(url).endsWith('/floor')) {
                floors++;
                return new Response(JSON.stringify(demo), { status: 200 });
            }
            return streamOf(['event: hello\ndata: {}\n\n', ...Array.from({ length: 20 }, () => 'event: floor\ndata: {}\n\n')]);
        }) as unknown as typeof fetch;
        const c = startClient({ key: KEY, fetch: f, onSnapshot: () => {}, onFailure: () => {} });
        await vi.advanceTimersByTimeAsync(240);
        c.stop();
        expect(floors).toBeLessThanOrEqual(1);
    });

    it('says denied on a 401 and stops', async () => {
        const kinds: string[] = [];
        let n = 0;
        const f = (async () => {
            n++;
            return new Response('{}', { status: 401 });
        }) as unknown as typeof fetch;
        startClient({ key: KEY, fetch: f, onSnapshot: () => {}, onFailure: (k) => kinds.push(k) });
        await vi.advanceTimersByTimeAsync(5000);
        expect(kinds[0]).toBe('denied');
        const after = n;
        await vi.advanceTimersByTimeAsync(60000);
        expect(n).toBe(after);
    });

    it('says failed on a server error or a body that is not a snapshot, and reconnects with back-off', async () => {
        const kinds: string[] = [];
        let streams = 0;
        const f = (async (url: string) => {
            if (String(url).endsWith('/floor')) return new Response('{"x":1}', { status: 200 });
            streams++;
            throw new Error('down');
        }) as unknown as typeof fetch;
        const c = startClient({ key: KEY, fetch: f, onSnapshot: () => {}, onFailure: (k) => kinds.push(k) });
        await vi.advanceTimersByTimeAsync(1000);
        expect(kinds).toContain('failed');
        const s1 = streams;
        await vi.advanceTimersByTimeAsync(10000);
        const s2 = streams;
        expect(s2).toBeGreaterThan(s1);
        expect(s2 - s1).toBeLessThanOrEqual(4); // 1 s, 2 s, 4 s: not a tight loop
        c.stop();
    });
});
