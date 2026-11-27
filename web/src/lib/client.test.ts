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

describe('SseParser: splits and caps', () => {
    const text = 'event: hello\r\ndata: {}\r\n\r\n: ping\n\nid: 5\nevent: journal\ndata: a\ndata: b\n\nevent: reset\r\r\revent: floor\ndata: x\r\n\r\n';
    it('gives the same events at every split position, and byte by byte', () => {
        const whole = new SseParser().feed(text);
        expect(whole.length).toBeGreaterThan(2);
        for (let i = 1; i < text.length; i++) {
            const p = new SseParser();
            expect([...p.feed(text.slice(0, i)), ...p.feed(text.slice(i))], 'split ' + i).toEqual(whole);
        }
        const p = new SseParser();
        const bytes: ReturnType<SseParser['feed']> = [];
        for (const ch of text) bytes.push(...p.feed(ch));
        expect(bytes).toEqual(whole);
    });
    it('refuses a line over 64 KiB', () => {
        const p = new SseParser();
        expect(() => { for (let i = 0; i < 3; i++) p.feed('x'.repeat(40000)); }).toThrow();
    });
    it('refuses more than 1 MiB held without an event', () => {
        const p = new SseParser();
        expect(() => { for (let i = 0; i < 20000; i++) p.feed('data: ' + 'y'.repeat(100) + '\n'); }).toThrow();
    });
});

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

    it('retries a failed snapshot with back-off while the stream stays open, and shows the good one when it comes', async () => {
        let snaps = 0;
        const fails: string[] = [];
        let got = 0;
        const enc = new TextEncoder();
        const body = new ReadableStream<Uint8Array>({ start(c) { c.enqueue(enc.encode('event: hello\ndata: {}\n\n')); } });
        const f = (async (u: string) => {
            if (String(u).endsWith('/events')) return new Response(body, { status: 200 });
            snaps++;
            return snaps <= 2 ? new Response('{}', { status: 500 }) : new Response(JSON.stringify(demo), { status: 200 });
        }) as unknown as typeof fetch;
        const c = startClient({ key: KEY, fetch: f, onSnapshot: () => got++, onFailure: (k) => fails.push(k), random: () => 0 });
        await vi.advanceTimersByTimeAsync(30000);
        c.stop();
        expect(fails).toEqual(['failed', 'failed']);
        expect(got).toBe(1);
        expect(snaps).toBe(3);
    });

    it('rejects a snapshot that is not valid and retries', async () => {
        let snaps = 0;
        const fails: string[] = [];
        const bad = structuredClone(demo) as any;
        bad.calls[0].kind = 'bogus';
        const f = (async (u: string) => {
            if (String(u).endsWith('/events')) throw new Error('down');
            snaps++;
            return new Response(JSON.stringify(bad), { status: 200 });
        }) as unknown as typeof fetch;
        const c = startClient({ key: KEY, fetch: f, onSnapshot: () => { throw new Error('must not be shown'); }, onFailure: (k) => fails.push(k), random: () => 0 });
        await vi.advanceTimersByTimeAsync(20000);
        c.stop();
        expect(fails.length).toBeGreaterThan(2);
        expect(snaps).toBeGreaterThan(2);
    });

    it('does not reset the back-off on hello: a stream that dies right after hello reconnects with growing delays', async () => {
        const opens: number[] = [];
        const enc = new TextEncoder();
        const f = (async (u: string) => {
            if (String(u).endsWith('/events')) {
                opens.push(Date.now());
                const body = new ReadableStream<Uint8Array>({ start(c) { c.enqueue(enc.encode('event: hello\ndata: {}\n\n')); c.close(); } });
                return new Response(body, { status: 200 });
            }
            return new Response(JSON.stringify(demo), { status: 200 });
        }) as unknown as typeof fetch;
        const c = startClient({ key: KEY, fetch: f, onSnapshot: () => {}, onFailure: () => {}, random: () => 0 });
        await vi.advanceTimersByTimeAsync(600000);
        c.stop();
        const gaps = opens.slice(1).map((t, i) => t - opens[i]!);
        expect(gaps.slice(0, 4)).toEqual([1000, 2000, 4000, 8000]);
        expect(Math.max(...gaps)).toBeLessThanOrEqual(30000);
        expect(opens.length).toBeLessThan(30);
    });

    it('drops a stream that sends an endless line and reconnects with back-off', async () => {
        let opens = 0;
        const f = (async (u: string) => {
            if (String(u).endsWith('/events')) {
                opens++;
                let sent = 0;
                const body = new ReadableStream<Uint8Array>({ pull(c) { if (sent++ < 100) c.enqueue(new TextEncoder().encode('x'.repeat(65536))); else return new Promise(() => {}); } });
                return new Response(body, { status: 200 });
            }
            return new Response(JSON.stringify(demo), { status: 200 });
        }) as unknown as typeof fetch;
        const c = startClient({ key: KEY, fetch: f, onSnapshot: () => {}, onFailure: () => {}, random: () => 0 });
        await vi.advanceTimersByTimeAsync(10000);
        c.stop();
        expect(opens).toBeGreaterThanOrEqual(2);
        expect(opens).toBeLessThan(6);
    });

    it('a server that is away is not hammered', async () => {
        let n = 0;
        const f = (async () => { n++; throw new TypeError('down'); }) as unknown as typeof fetch;
        const c = startClient({ key: KEY, fetch: f, onSnapshot: () => {}, onFailure: () => {}, random: () => 0 });
        await vi.advanceTimersByTimeAsync(600000);
        c.stop();
        expect(n).toBeLessThan(80);
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
