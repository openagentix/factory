// Helpers for the tests: a fake page environment and a fake server. No network and no browser are used.
import { render } from '@testing-library/svelte';
import { vi } from 'vitest';
import { tick } from 'svelte';
import demo from '../fixtures/floor.demo.json';
import notbuilt from '../fixtures/floor.notbuilt.json';
import paused from '../fixtures/floor.paused.json';
import unknown from '../fixtures/floor.unknown.json';
import type { Env } from './state.svelte';
import Shell from './Shell.svelte';

export const KEY = 'abcDEF0123456789_-abcDEF0123456789_-xyzXYZ';
export const FIXTURES = { demo, unknown, notbuilt, paused } as const;
export type Json = Record<string, unknown>;

export type Fake = {
    env: Env;
    store: Map<string, string>;
    calls: { url: string; headers: Record<string, string> }[];
    urls: string[];
    /** Change what the next snapshot read returns. A number is a status code. */
    setSnapshot(v: unknown): void;
    /** Send a server event on the stream. */
    event(name: string): void;
    perf: { now: number };
    served: () => number;
};

export function fakeEnv(opts: { snapshot?: unknown; hash?: string; stored?: string | null } = {}): Fake {
    const store = new Map<string, string>();
    if (opts.stored) store.set('oxf-key', opts.stored);
    const local = new Map<string, string>();
    let snap: unknown = opts.snapshot ?? demo;
    const calls: Fake['calls'] = [];
    const urls: string[] = [];
    let ctl: ReadableStreamDefaultController<Uint8Array> | null = null;
    const enc = new TextEncoder();
    const perf = { now: 0 };
    const location = { hash: opts.hash ?? '', pathname: '/', search: '' };
    const env: Env = {
        fetch: (async (url: string, init?: RequestInit) => {
            calls.push({ url: String(url), headers: { ...(init?.headers as Record<string, string>) } });
            if (String(url).endsWith('/events')) {
                const body = new ReadableStream<Uint8Array>({
                    start(c) {
                        ctl = c;
                        c.enqueue(enc.encode('event: hello\ndata: {}\n\n'));
                    },
                });
                return new Response(body, { status: 200 });
            }
            if (typeof snap === 'number') return new Response('{}', { status: snap });
            return new Response(JSON.stringify(snap), { status: 200 });
        }) as unknown as typeof fetch,
        location,
        history: { replaceState: (_d, _u, url) => void urls.push(String(url)) },
        sessionStorage: { getItem: (k) => store.get(k) ?? null, setItem: (k, v) => void store.set(k, v) },
        localStorage: { getItem: (k) => local.get(k) ?? null, setItem: (k, v) => void local.set(k, v) },
        perfNow: () => perf.now,
        matchMedia: () => ({ matches: false }),
    };
    return {
        env, store, calls, urls, perf,
        served: () => calls.filter((c) => c.url.endsWith('/floor')).length,
        setSnapshot: (v) => void (snap = v),
        event: (name) => ctl?.enqueue(enc.encode(`event: ${name}\ndata: {}\n\n`)),
    };
}

export function clone<T>(v: T): T {
    return structuredClone(v);
}

/** Mounts the page with the given fixture, and waits for the first snapshot to be drawn. */
export async function mount(snapshot: unknown, route = 'floor') {
    const fake = fakeEnv({ snapshot, hash: '#' + route, stored: KEY });
    const r = render(Shell, { props: { env: fake.env } });
    // Wait for the snapshot to be read, then for the page to draw it.
    await vi.waitFor(() => {
        if (fake.served() < 1) throw new Error('not read yet');
    });
    await vi.waitFor(() => {
        if (r.container.textContent?.includes('Reading the factory')) throw new Error('not drawn yet');
    });
    await tick();
    await new Promise((res) => setTimeout(res, 15));
    return { ...r, fake };
}
