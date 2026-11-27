// The first three buttons: answer, yes or no, approve. Nothing the page does changes the screen by itself: only a later
// snapshot does.
import { fireEvent } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import phrases from './phrases.json';
import { startClient } from './client';
import { FIXTURES, KEY, clone, fakeEnv, mount } from './testkit';

type Any = any;
const txt = (e: Element | null) => (e?.textContent ?? '').replace(/\s+/g, ' ').trim();
const table = phrases as Record<string, string>;
const ok = (result = 'done') => new Response(JSON.stringify({ mode: 'demo', result, journalSeq: 3 }), { status: 200 });
const refuse = (status: number, code: string) => new Response(JSON.stringify({ error: code }), { status });

async function mountWrites(write: (url: string, init: RequestInit) => Response | Promise<Response>, snapshot: unknown = FIXTURES.demo) {
    const fake = fakeEnv({ snapshot, hash: '#needs-you', stored: KEY, write });
    const { render } = await import('@testing-library/svelte');
    const Shell = (await import('./Shell.svelte')).default;
    const r = render(Shell, { props: { env: fake.env } });
    await vi.waitFor(() => expect(r.container.querySelector('.call')).not.toBeNull());
    return { ...r, fake };
}

const btn = (c: HTMLElement, label: string) => [...c.querySelectorAll<HTMLButtonElement>('button')].find((b) => txt(b) === label)!;
const call = (c: HTMLElement, id: string) => c.querySelector<HTMLElement>(`[data-call="${id}"]`)!;
const box = (c: HTMLElement) => c.querySelector<HTMLTextAreaElement>('textarea')!;

describe('the buttons that are built', () => {
    it('a button stays out when the call does not list the action', async () => {
        const snap: Any = clone(FIXTURES.demo);
        for (const c of snap.calls) {
            if (c.kind === 'consequence') c.actions = ['yes'];
            if (c.kind === 'question') c.actions = [];
            if (c.kind === 'plan') c.actions = [];
        }
        const { container } = await mountWrites(() => ok(), snap);
        expect(btn(container, 'No, send it back')).toBeUndefined();
        expect(btn(container, 'Yes, that is what I want')).toBeDefined();
        expect(btn(container, 'Send answer')).toBeUndefined();
        expect(btn(container, 'Approve the plan')).toBeUndefined();
        expect(box(container).disabled).toBe(true);
    });

    it('an answer is one POST with the version and the text, and the call stays until a snapshot removes it', async () => {
        const { container, fake } = await mountWrites(() => ok());
        const a = box(container);
        await fireEvent.input(a, { target: { value: 'Sooner is fine.' } });
        const send = btn(container, 'Send answer');
        expect(send.getAttribute('aria-disabled')).toBeNull();
        await userEvent.click(send);
        await vi.waitFor(() => expect(txt(call(container, 'q-58-9001').querySelector('.act-msg'))).toContain('Sent.'));
        expect(fake.writes).toHaveLength(1);
        const w = fake.writes[0]!;
        expect(w.url).toBe('/api/v1/calls/q-58-9001/answer');
        expect(JSON.parse(w.body)).toEqual({ text: 'Sooner is fine.', version: (FIXTURES.demo as Any).calls.find((c: Any) => c.id === 'q-58-9001').version });
        expect(w.init.credentials).toBe('omit');
        expect(w.init.cache).toBe('no-store');
        expect(w.init.redirect).toBe('error');
        expect(w.headers.Authorization).toBe('Bearer ' + KEY);
        expect(w.headers['Content-Type']).toBe('application/json');
        // The page read the snapshot again, got the same one, and keeps the call: no optimistic update.
        await vi.waitFor(() => expect(fake.served()).toBeGreaterThan(1));
        expect(call(container, 'q-58-9001')).not.toBeNull();
        // A snapshot that still has the call at the same version gives the person the button back, with the same key.
        await vi.waitFor(() => expect(btn(container, 'Send answer').getAttribute('aria-disabled')).toBeNull());
        await userEvent.click(btn(container, 'Send answer'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(2));
        expect(fake.writes[1]!.headers['Idempotency-Key']).toBe(fake.writes[0]!.headers['Idempotency-Key']);
        expect(fake.writes[1]!.body).toBe(fake.writes[0]!.body);
        // Only a snapshot without the call at that version removes it.
        const next: Any = clone(FIXTURES.demo);
        next.calls = next.calls.filter((c: Any) => c.id !== 'q-58-9001');
        for (const p of next.pieces) if (p.call === 'q-58-9001') p.call = null;
        fake.setSnapshot(next);
        fake.event('floor');
        await vi.waitFor(() => expect(container.querySelector('[data-call="q-58-9001"]')).toBeNull());
    });

    it('yes and no and approve post to their own routes with the version only', async () => {
        const { container, fake } = await mountWrites(() => ok());
        const ver = (id: string) => (FIXTURES.demo as Any).calls.find((c: Any) => c.id === id).version;
        await userEvent.click(btn(container, 'Yes, that is what I want'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        expect(fake.writes[0]!.url).toBe('/api/v1/calls/c-55-r_55a/yes');
        expect(JSON.parse(fake.writes[0]!.body)).toEqual({ version: ver('c-55-r_55a') });
        await userEvent.click(btn(container, 'Approve the plan'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(2));
        expect(fake.writes[1]!.url).toBe('/api/v1/calls/p-44/approve');
        expect(JSON.parse(fake.writes[1]!.body)).toEqual({ version: ver('p-44') });
    });

    it('a no goes to the no route', async () => {
        const { container, fake } = await mountWrites(() => ok());
        await userEvent.click(btn(container, 'No, send it back'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        expect(fake.writes[0]!.url).toBe('/api/v1/calls/c-55-r_55a/no');
    });

    it('a refusal shows the fixed sentence of its code, keeps the call and keeps the typed text', async () => {
        for (const [status, code] of [[409, 'stale'], [409, 'in_progress'], [502, 'host_failed'], [422, 'unprocessable'], [413, 'payload_too_large'], [503, 'unavailable'], [400, 'bad_request'], [500, 'internal']] as const) {
            const { container, unmount } = await mountWrites(() => refuse(status, code));
            await fireEvent.input(box(container), { target: { value: 'my words' } });
            await userEvent.click(btn(container, 'Send answer'));
            await vi.waitFor(() => expect(txt(call(container, 'q-58-9001').querySelector('.act-msg'))).toBe(table['refusal.' + code]));
            expect(box(container).value).toBe('my words');
            expect(call(container, 'q-58-9001')).not.toBeNull();
            // The button works again: a refusal is not a sent press.
            expect(btn(container, 'Send answer').getAttribute('aria-disabled')).toBeNull();
            unmount();
        }
    });

    it('an unknown code reads as a fixed sentence, never as the server wrote it', async () => {
        const { container } = await mountWrites(() => new Response('{"error":"<img src=x>"}', { status: 418 }));
        await userEvent.click(btn(container, 'Yes, that is what I want'));
        await vi.waitFor(() => expect(txt(call(container, 'c-55-r_55a').querySelector('.act-msg'))).toBe(table['refusal.internal']));
        expect(container.querySelector('img')).toBeNull();
    });

    it('no answer at all says so, and the call stays', async () => {
        const { container } = await mountWrites(() => Promise.reject(new TypeError('network')));
        await userEvent.click(btn(container, 'Approve the plan'));
        await vi.waitFor(() => expect(txt(call(container, 'p-44').querySelector('.act-msg'))).toContain('did not answer'));
        expect(call(container, 'p-44')).not.toBeNull();
    });

    it('typed text is sent as text and never becomes markup', async () => {
        const { container, fake } = await mountWrites(() => ok());
        const evil = '<img src=x onerror=alert(1)><script>boom()</script>';
        await fireEvent.input(box(container), { target: { value: evil } });
        await userEvent.click(btn(container, 'Send answer'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        expect(JSON.parse(fake.writes[0]!.body).text).toBe(evil);
        expect(container.querySelector('img')).toBeNull();
        expect(container.querySelector('script')).toBeNull();
    });

    it('line ends are sent as line feeds, and the counter counts bytes', async () => {
        const { container, fake } = await mountWrites(() => ok());
        await fireEvent.input(box(container), { target: { value: 'é' } });
        expect(txt(container.querySelector('#n-q-58-9001'))).toBe('2 of 4,096');
        await fireEvent.input(box(container), { target: { value: 'a\r\nb\rc' } });
        await userEvent.click(btn(container, 'Send answer'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        expect(JSON.parse(fake.writes[0]!.body).text).toBe('a\nb\nc');
    });

    it('send waits for words and for a text inside the limit', async () => {
        const { container, fake } = await mountWrites(() => ok());
        const send = () => btn(container, 'Send answer');
        expect(send().getAttribute('aria-disabled')).toBe('true');
        await userEvent.click(send());
        await fireEvent.input(box(container), { target: { value: '   ' } });
        await userEvent.click(send());
        await fireEvent.input(box(container), { target: { value: 'a'.repeat(4097) } });
        expect(txt(container.querySelector('#n-q-58-9001'))).toBe('4,097 of 4,096');
        await userEvent.click(send());
        expect(fake.writes).toHaveLength(0);
        await fireEvent.input(box(container), { target: { value: 'a'.repeat(4096) } });
        expect(send().getAttribute('aria-disabled')).toBeNull();
    });

    it('an answer with nothing to see keeps Send switched off', async () => {
        const { container, fake } = await mountWrites(() => ok());
        for (const ch of ['\u3164', '\u2800', '\u115F', '\uFFA0', '\u034F', '\uFE0F', '\u{E0100}', '\u200B', '\u00A0', ' \n\t']) {
            await fireEvent.input(box(container), { target: { value: ch + ch } });
            const send = btn(container, 'Send answer');
            expect(send.getAttribute('aria-disabled'), JSON.stringify(ch)).toBe('true');
            await userEvent.click(send);
        }
        expect(fake.writes).toHaveLength(0);
        await fireEvent.input(box(container), { target: { value: 'a\u3164' } });
        expect(btn(container, 'Send answer').getAttribute('aria-disabled')).toBeNull();
    });

    it('focus stays on the pressed button while the press is sent', async () => {
        let release: (r: Response) => void = () => {};
        const { container } = await mountWrites(() => new Promise<Response>((res) => (release = res)));
        const b = btn(container, 'Yes, that is what I want');
        await userEvent.click(b);
        await vi.waitFor(() => expect(b.getAttribute('aria-disabled')).toBe('true'));
        expect(document.activeElement).toBe(b);
        expect(txt(call(container, 'c-55-r_55a').querySelector('.act-msg'))).toBe('Sending.');
        release(ok());
        await vi.waitFor(() => expect(txt(call(container, 'c-55-r_55a').querySelector('.act-msg'))).toContain('Sent.'));
        expect(document.activeElement).toBe(b);
    });

    it('a repeat press of the same call sends the same key, a changed text a new one, and the key is not the login key', async () => {
        const { container, fake } = await mountWrites(() => refuse(502, 'host_failed'));
        await fireEvent.input(box(container), { target: { value: 'one' } });
        await userEvent.click(btn(container, 'Send answer'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        await vi.waitFor(() => expect(txt(call(container, 'q-58-9001').querySelector('.act-msg'))).toBe(table['refusal.host_failed']));
        await userEvent.click(btn(container, 'Send answer'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(2));
        await fireEvent.input(box(container), { target: { value: 'two' } });
        await vi.waitFor(() => expect(txt(call(container, 'q-58-9001').querySelector('.act-msg'))).toBe(table['refusal.host_failed']));
        await userEvent.click(btn(container, 'Send answer'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(3));
        const keys = fake.writes.map((w) => w.headers['Idempotency-Key']!);
        for (const k of keys) {
            expect(k).toMatch(/^[A-Za-z0-9_-]{16,64}$/);
            expect(k).not.toContain(KEY.slice(0, 12));
            expect(KEY).not.toContain(k);
        }
        expect(keys[1]).toBe(keys[0]);
        expect(keys[2]).not.toBe(keys[0]);
        for (const u of fake.calls.map((c) => c.url).concat(fake.writes.map((w) => w.url))) expect(u).not.toContain(KEY);
    });

    it('moves focus to the next call when the one that held it goes', async () => {
        const { container, fake } = await mountWrites(() => ok());
        const q = call(container, 'q-58-9001');
        q.querySelector<HTMLElement>('textarea')!.focus();
        const next: Any = clone(FIXTURES.demo);
        next.calls = next.calls.filter((c: Any) => c.id !== 'q-58-9001');
        for (const p of next.pieces) if (p.call === 'q-58-9001') p.call = null;
        fake.setSnapshot(next);
        fake.event('floor');
        await vi.waitFor(() => expect(container.querySelector('[data-call="q-58-9001"]')).toBeNull());
        await vi.waitFor(() => expect(document.activeElement?.closest('.call') ?? document.activeElement?.closest('.calls')).not.toBeNull());
        expect(document.activeElement).not.toBe(document.body);
    });
});

describe('a press that is accepted and the call stays', () => {
    it('after a 202 the page says it is being done, and a later snapshot that still has the call gives the button back with the same key', async () => {
        const { container, fake } = await mountWrites(() => new Response('{"mode":"demo","result":"running"}', { status: 202 }));
        await userEvent.click(btn(container, 'Yes, that is what I want'));
        const msg = () => txt(call(container, 'c-55-r_55a').querySelector('.act-msg'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        await vi.waitFor(() => expect(msg()).toContain('being done'));
        // The write asked for a snapshot. It still has the call at the same version.
        await vi.waitFor(() => expect(fake.served()).toBeGreaterThan(1));
        await vi.waitFor(() => expect(btn(container, 'Yes, that is what I want').getAttribute('aria-disabled')).toBeNull());
        expect(call(container, 'c-55-r_55a')).not.toBeNull(); // no optimistic change
        await userEvent.click(btn(container, 'Yes, that is what I want'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(2));
        expect(fake.writes[1]!.headers['Idempotency-Key']).toBe(fake.writes[0]!.headers['Idempotency-Key']);
        // Once the call is gone from a snapshot the key goes too: a call at a new version is a new press.
        const next: Any = clone(FIXTURES.demo);
        const c = next.calls.find((x: Any) => x.id === 'c-55-r_55a');
        c.version = 'f'.repeat(64);
        const seen = fake.served();
        fake.setSnapshot(next);
        fake.event('floor');
        await vi.waitFor(() => expect(fake.served()).toBeGreaterThan(seen));
        await vi.waitFor(async () => {
            await userEvent.click(btn(container, 'Yes, that is what I want'));
            expect(fake.writes.length).toBeGreaterThanOrEqual(3);
        });
        expect(fake.writes[2]!.headers['Idempotency-Key']).not.toBe(fake.writes[0]!.headers['Idempotency-Key']);
        expect(JSON.parse(fake.writes[2]!.body).version).toBe('f'.repeat(64));
    });

    it('a press that is still being sent is not sent twice', async () => {
        let release: (r: Response) => void = () => {};
        const { container, fake } = await mountWrites(() => new Promise<Response>((res) => (release = res)));
        const b = btn(container, 'Approve the plan');
        await userEvent.click(b);
        await userEvent.click(b);
        expect(fake.writes).toHaveLength(1);
        release(ok());
    });
});

describe('the write path of the client', () => {
    it('a 401 stops the client and reports denied', async () => {
        const onFailure = vi.fn();
        const f = vi.fn(async (url: string, init?: RequestInit) => {
            if (init?.method === 'POST') return new Response('{"error":"unauthorized"}', { status: 401 });
            return new Response('{}', { status: 500 });
        });
        const c = startClient({ key: KEY, fetch: f as unknown as typeof fetch, onSnapshot: () => {}, onFailure, minGapMs: 0 });
        const r = await c.write('/api/v1/calls/p-1/approve', { version: 'x' }, 'k'.repeat(16));
        expect(r).toEqual({ ok: false, status: 401, code: 'unauthorized' });
        expect(onFailure).toHaveBeenCalledWith('denied');
        c.stop();
    });

    it('a 200 with an unknown result is not believed', async () => {
        const f = vi.fn(async () => new Response('{"result":"maybe"}', { status: 200 }));
        const c = startClient({ key: KEY, fetch: f as unknown as typeof fetch, onSnapshot: () => {}, onFailure: () => {}, minGapMs: 0 });
        const r = await c.write('/api/v1/calls/p-1/approve', { version: 'x' }, 'k'.repeat(16));
        expect(r.ok).toBe(false);
        c.stop();
    });
});
