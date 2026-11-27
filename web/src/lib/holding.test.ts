// The Accept call's buttons and the piece drawer: Accept asks once, Cancel only while an Accept is pending, the drawer
// reads the piece, and Undo shows what leaves before it asks. Nothing the page does changes the screen by itself.
import { fireEvent } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import phrases from './phrases.json';
import { FIXTURES, KEY, clone, fakeEnv } from './testkit';

type Any = any;
const txt = (e: Element | null) => (e?.textContent ?? '').replace(/\s+/g, ' ').trim();
const table = phrases as Record<string, string>;
const ok = (result = 'done') => new Response(JSON.stringify({ mode: 'demo', result, journalSeq: 3 }), { status: 200 });
const refuse = (status: number, code: string) => new Response(JSON.stringify({ error: code }), { status });
const json = (v: unknown, status = 200) => new Response(JSON.stringify(v), { status });
const C40 = 'c'.repeat(40);

const pieceOf = (id: string) => (FIXTURES.demo as Any).pieces.find((p: Any) => p.id === id);
function detailOf(id: string, extra: Record<string, unknown> = {}) {
    return {
        schemaVersion: 1, mode: 'demo', generatedAt: '2026-11-27T12:00:00Z', piece: pieceOf(id),
        route: [{ station: 3, at: '2026-11-27T10:00:00Z' }, { station: 6, at: null }],
        rulings: [{ text: 'Kept the old limit <img src=x onerror=alert(1)>', truncated: false }],
        unconfirmed: [{ text: 'Line 3 has no check', truncated: false }],
        undo: { change: 'run-46', stagingSeq: 7, commit: C40 }, ...extra,
    };
}
const previewOf = (extra: Record<string, unknown> = {}) => ({
    mode: 'demo', change: 'run-46', stagingSeq: 7, commit: C40, alsoRemoved: ['run-47'],
    tickets: [{ change: 'run-46', ticket: 46 }, { change: 'run-47', ticket: 47 }], ...extra,
});

type Opts = { snapshot?: unknown; write?: (u: string, i: RequestInit) => Response | Promise<Response>; read?: (u: string) => Response | Promise<Response> };
async function mountAt(route: string, o: Opts = {}) {
    const fake = fakeEnv({ snapshot: o.snapshot ?? FIXTURES.demo, hash: '#' + route, stored: KEY, write: o.write ?? (() => ok()), read: o.read });
    const { render } = await import('@testing-library/svelte');
    const Shell = (await import('./Shell.svelte')).default;
    const r = render(Shell, { props: { env: fake.env } });
    await vi.waitFor(() => expect(r.container.querySelector('.tag, .call')).not.toBeNull());
    return { ...r, fake };
}
const btn = (c: ParentNode, label: string) => [...c.querySelectorAll<HTMLButtonElement>('button')].find((b) => txt(b) === label)!;
const acceptCall = (c: ParentNode) => c.querySelector<HTMLElement>('[data-call="a-7"]')!;
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"]')!;
const acceptVersion = () => (FIXTURES.demo as Any).calls.find((c: Any) => c.id === 'a-7').version;

describe('Accept', () => {
    it('asks once before it acts, and Back or Escape goes back with no write', async () => {
        const { container, fake } = await mountAt('needs-you');
        const accept = btn(acceptCall(container), 'Accept');
        expect(accept.disabled).toBe(false);
        await userEvent.click(accept);
        const group = await vi.waitFor(() => {
            const g = acceptCall(container).querySelector<HTMLElement>('[role="group"]');
            if (!g) throw new Error('no confirm');
            return g;
        });
        expect(txt(group)).toContain('Accept everything in the holding area');
        expect(txt(group)).toContain('3 changes in 1 goal');
        expect(group.contains(document.activeElement)).toBe(true);
        expect(btn(group, 'Accept now')).toBeDefined();
        expect(fake.writes).toHaveLength(0);
        await userEvent.click(btn(group, 'Back'));
        await vi.waitFor(() => expect(acceptCall(container).querySelector('[role="group"]')).toBeNull());
        expect(document.activeElement).toBe(btn(acceptCall(container), 'Accept'));
        // Escape does the same.
        await userEvent.click(btn(acceptCall(container), 'Accept'));
        await vi.waitFor(() => expect(acceptCall(container).querySelector('[role="group"]')).not.toBeNull());
        await userEvent.keyboard('{Escape}');
        await vi.waitFor(() => expect(acceptCall(container).querySelector('[role="group"]')).toBeNull());
        expect(document.activeElement).toBe(btn(acceptCall(container), 'Accept'));
        expect(fake.writes).toHaveLength(0);
    });

    it('Accept now is one POST with the state and the version, and the call stays until a snapshot removes it', async () => {
        const { container, fake } = await mountAt('needs-you');
        await userEvent.click(btn(acceptCall(container), 'Accept'));
        await userEvent.click(btn(acceptCall(container), 'Accept now'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        const w = fake.writes[0]!;
        expect(w.url).toBe('/api/v1/holding-area/accept');
        expect(JSON.parse(w.body)).toEqual({ stagingSeq: 7, commit: 'ab12cd34', version: acceptVersion() });
        expect(w.headers['Idempotency-Key']).toMatch(/^[A-Za-z0-9_-]{16,64}$/);
        expect(w.headers['Idempotency-Key']).not.toBe(KEY);
        expect(w.init.credentials).toBe('omit');
        await vi.waitFor(() => expect(txt(acceptCall(container).querySelector('.act-msg'))).toContain('Sent.'));
        expect(acceptCall(container)).not.toBeNull();
        // The confirm is closed and the button cannot be pressed twice while it is sent.
        expect(acceptCall(container).querySelector('[role="group"]')).toBeNull();
        // Only a snapshot without the call removes it.
        const next: Any = clone(FIXTURES.demo);
        next.calls = next.calls.filter((c: Any) => c.id !== 'a-7');
        fake.setSnapshot(next);
        fake.event('floor');
        await vi.waitFor(() => expect(container.querySelector('[data-call="a-7"]')).toBeNull());
    });

    it('a refusal shows the fixed sentence and keeps the call', async () => {
        for (const [status, code] of [[409, 'stale'], [502, 'host_failed'], [503, 'unavailable']] as const) {
            const { container, unmount } = await mountAt('needs-you', { write: () => refuse(status, code) });
            await userEvent.click(btn(acceptCall(container), 'Accept'));
            await userEvent.click(btn(acceptCall(container), 'Accept now'));
            await vi.waitFor(() => expect(txt(acceptCall(container).querySelector('.act-msg'))).toBe(table['refusal.' + code]));
            expect(acceptCall(container)).not.toBeNull();
            unmount();
        }
    });

    it('a pending Accept shows no Accept, and Cancel only when the call lists it, with no confirm', async () => {
        const snap: Any = clone(FIXTURES.demo);
        const c = snap.calls.find((x: Any) => x.id === 'a-7');
        c.actions = ['cancel'];
        snap.holdingArea.frozen = true;
        snap.holdingArea.pendingAccept = { since: '2026-11-27T11:50:00Z', mode: 'merge', pullRequest: 3 };
        const { container, fake } = await mountAt('needs-you', { snapshot: snap });
        expect(btn(acceptCall(container), 'Accept')).toBeUndefined();
        await userEvent.click(btn(acceptCall(container), 'Cancel the Accept'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        expect(fake.writes[0]!.url).toBe('/api/v1/holding-area/cancel');
        expect(JSON.parse(fake.writes[0]!.body)).toEqual({ stagingSeq: 7, commit: 'ab12cd34', version: c.version });
        // And with no cancel listed there is no Cancel.
        c.actions = [];
        const again = await mountAt('needs-you', { snapshot: snap });
        expect(btn(acceptCall(again.container), 'Cancel the Accept')).toBeUndefined();
    });

    it('nothing is offered when the call does not list accept', async () => {
        const snap: Any = clone(FIXTURES.demo);
        snap.calls.find((x: Any) => x.id === 'a-7').actions = [];
        const { container } = await mountAt('needs-you', { snapshot: snap });
        expect(btn(acceptCall(container), 'Accept')).toBeUndefined();
        expect(btn(acceptCall(container), 'Accept now')).toBeUndefined();
    });

    it('a call that is not the holding area as it stands sends nothing', async () => {
        const snap: Any = clone(FIXTURES.demo);
        snap.holdingArea.stagingSeq = 8; // the call says a-7
        const { container, fake } = await mountAt('needs-you', { snapshot: snap });
        await userEvent.click(btn(acceptCall(container), 'Accept'));
        await userEvent.click(btn(acceptCall(container), 'Accept now'));
        await vi.waitFor(() => expect(txt(acceptCall(container).querySelector('.act-msg'))).toBe(table['refusal.stale']));
        expect(fake.writes).toHaveLength(0);
    });

    it('the unbuilt buttons stay disabled with Not built yet, and the old Undo button is a sentence', async () => {
        const { container } = await mountAt('needs-you');
        const c = acceptCall(container);
        const tryIt = btn(c, 'Try it');
        expect(tryIt.disabled).toBe(true);
        expect(txt(tryIt.parentElement!)).toContain('Not built yet');
        expect(btn(c, 'Undo a change')).toBeUndefined();
        expect(txt(c)).toContain('To take a change back, open it on the line.');
    });
});

describe('the piece drawer', () => {
    async function openPiece(num: number, o: Opts = {}) {
        const m = await mountAt('floor', { read: (u) => (u.endsWith('/pieces/t' + num) ? json(detailOf('t' + num)) : refuse(404, 'not_found')), ...o });
        const tag = [...m.container.querySelectorAll<HTMLButtonElement>('.tag')].find((t) => txt(t).includes('#' + num))!;
        tag.focus();
        await userEvent.keyboard('{Enter}');
        await vi.waitFor(() => expect(dialog()).not.toBeNull());
        return { ...m, tag };
    }

    it('reads the piece from the server and shows its route, rulings and what is not confirmed', async () => {
        const { fake } = await openPiece(46);
        await vi.waitFor(() => expect(txt(dialog())).toContain('Kept the old limit'));
        expect(fake.reads.map((r) => r.url)).toContain('/api/v1/pieces/t46');
        expect(fake.reads[0]!.headers.Authorization).toBe('Bearer ' + KEY);
        const heads = [...dialog().querySelectorAll('h3')].map(txt);
        expect(heads).toEqual(['Its route so far', 'Decided for you', 'Could not be confirmed']);
        expect(txt(dialog())).toContain('Line 3 has no check');
        expect(txt(dialog())).not.toContain('Not built yet');
        expect(txt(dialog())).toMatch(/03 Build/);
        // Text from an agent is text: the hostile markup is not an element.
        expect(dialog().querySelector('img')).toBeNull();
        expect(txt(dialog())).toContain('<img src=x onerror=alert(1)>');
        expect(txt(dialog())).toContain('Written by the agent, as written');
    });

    it('says Could not read when the detail cannot be read, never a pass', async () => {
        await openPiece(46, { read: () => refuse(500, 'read_failed') });
        await vi.waitFor(() => expect(txt(dialog())).toContain('Could not read'));
        expect(txt(dialog())).not.toContain('Not built yet');
        expect(btn(dialog(), 'Undo this change')).toBeUndefined();
    });

    it('a reply that is not whole is not shown', async () => {
        const bad = detailOf('t46');
        (bad as Any).rulings = 'oops';
        await openPiece(46, { read: () => json(bad) });
        await vi.waitFor(() => expect(txt(dialog())).toContain('Could not read'));
        expect(txt(dialog())).not.toContain('oops');
    });

    it('offers Undo only when the detail has the undo field', async () => {
        await openPiece(46, { read: () => json(detailOf('t46', { undo: null })) });
        await vi.waitFor(() => expect(txt(dialog())).toContain('Kept the old limit'));
        expect(btn(dialog(), 'Undo this change')).toBeUndefined();
    });

    it('a piece that was accepted has Undo after Accept, disabled, Not built yet', async () => {
        await openPiece(39, { read: () => json(detailOf('t39', { undo: null })) });
        await vi.waitFor(() => expect(txt(dialog())).toContain('Undo after Accept'));
        const b = btn(dialog(), 'Undo after Accept');
        expect(b.disabled).toBe(true);
        expect(txt(b.parentElement!)).toContain('Not built yet');
    });

    it('Undo shows what leaves, as ticket numbers, then asks, then sends one POST', async () => {
        const { fake } = await openPiece(46, { read: (u) => (u.endsWith('/pieces/t46') ? json(detailOf('t46')) : u.includes('/undo-preview') ? json(previewOf()) : refuse(404, 'not_found')) });
        await vi.waitFor(() => expect(btn(dialog(), 'Undo this change')).toBeDefined());
        await userEvent.click(btn(dialog(), 'Undo this change'));
        const panel = await vi.waitFor(() => {
            const p = dialog().querySelector<HTMLElement>('[data-undo]');
            if (!p || !btn(p, 'Undo now')) throw new Error('no preview yet');
            return p;
        });
        expect(fake.reads.map((r) => r.url)).toContain('/api/v1/changes/run-46/undo-preview');
        expect(txt(panel)).toContain('Ticket 46');
        expect(txt(panel)).toContain('Ticket 47');
        expect(txt(panel)).toContain('leave the holding area');
        // An entry id is never shown, not as text and not in a label.
        expect(dialog().innerHTML).not.toContain('run-46');
        expect(dialog().innerHTML).not.toContain('run-47');
        expect(panel.contains(document.activeElement)).toBe(true);
        expect(fake.writes).toHaveLength(0);
        await userEvent.click(btn(panel, 'Undo now'));
        await vi.waitFor(() => expect(fake.writes).toHaveLength(1));
        const w = fake.writes[0]!;
        expect(w.url).toBe('/api/v1/changes/run-46/undo');
        expect(JSON.parse(w.body)).toEqual({ stagingSeq: 7, commit: C40, change: 'run-46', alsoRemoved: ['run-47'] });
        expect(w.headers['Idempotency-Key']).toMatch(/^[A-Za-z0-9_-]{16,64}$/);
        await vi.waitFor(() => expect(txt(dialog().querySelector('[data-undo]'))).toContain('Sent.'));
        // Only a later snapshot, with another state, closes it.
        const next: Any = clone(FIXTURES.demo);
        next.holdingArea.stagingSeq = 8;
        next.holdingArea.commit = 'd'.repeat(40);
        fake.setSnapshot(next);
        fake.event('floor');
        await vi.waitFor(() => expect(dialog().querySelector('[data-undo]')).toBeNull());
    });

    it('a single change says that nothing else leaves with it', async () => {
        await openPiece(46, { read: (u) => (u.endsWith('/pieces/t46') ? json(detailOf('t46')) : json(previewOf({ alsoRemoved: [], tickets: [{ change: 'run-46', ticket: 46 }] }))) });
        await vi.waitFor(() => expect(btn(dialog(), 'Undo this change')).toBeDefined());
        await userEvent.click(btn(dialog(), 'Undo this change'));
        await vi.waitFor(() => expect(txt(dialog().querySelector('[data-undo]'))).toContain('Ticket 46 leaves the holding area. Nothing else leaves with it.'));
    });

    it('Back and Escape leave the preview without a write, and Escape does not close the drawer then', async () => {
        const { fake } = await openPiece(46, { read: (u) => (u.endsWith('/pieces/t46') ? json(detailOf('t46')) : json(previewOf())) });
        await vi.waitFor(() => expect(btn(dialog(), 'Undo this change')).toBeDefined());
        await userEvent.click(btn(dialog(), 'Undo this change'));
        await vi.waitFor(() => expect(btn(dialog(), 'Undo now')).toBeDefined());
        await userEvent.click(btn(dialog(), 'Back'));
        await vi.waitFor(() => expect(dialog().querySelector('[data-undo]')).toBeNull());
        expect(document.activeElement).toBe(btn(dialog(), 'Undo this change'));
        await userEvent.click(btn(dialog(), 'Undo this change'));
        await vi.waitFor(() => expect(btn(dialog(), 'Undo now')).toBeDefined());
        await userEvent.keyboard('{Escape}');
        await vi.waitFor(() => expect(dialog().querySelector('[data-undo]')).toBeNull());
        expect(dialog()).not.toBeNull(); // the drawer stays
        await userEvent.keyboard('{Escape}');
        await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
        expect(fake.writes).toHaveLength(0);
    });

    it('a refused preview shows the fixed sentence and no confirm', async () => {
        await openPiece(46, { read: (u) => (u.endsWith('/pieces/t46') ? json(detailOf('t46')) : refuse(409, 'stale')) });
        await vi.waitFor(() => expect(btn(dialog(), 'Undo this change')).toBeDefined());
        await userEvent.click(btn(dialog(), 'Undo this change'));
        await vi.waitFor(() => expect(txt(dialog().querySelector('[data-undo]'))).toContain(table['refusal.stale']));
        expect(btn(dialog(), 'Undo now')).toBeUndefined();
    });

    it('a preview for another change or another state, or one that is not whole, is not acted on', async () => {
        for (const bad of [previewOf({ change: 'run-99', tickets: [{ change: 'run-99', ticket: 1 }, { change: 'run-47', ticket: 47 }] }), previewOf({ stagingSeq: 6 }), previewOf({ commit: 'e'.repeat(40) }), previewOf({ tickets: [{ change: 'run-46', ticket: 46 }] })]) {
            const { fake, unmount } = await openPiece(46, { read: (u) => (u.endsWith('/pieces/t46') ? json(detailOf('t46')) : json(bad)) });
            await vi.waitFor(() => expect(btn(dialog(), 'Undo this change')).toBeDefined());
            await userEvent.click(btn(dialog(), 'Undo this change'));
            await vi.waitFor(() => expect(dialog().querySelector('[data-undo]')?.textContent).toMatch(/\S/));
            await vi.waitFor(() => expect(btn(dialog(), 'Back')).toBeDefined());
            expect(btn(dialog(), 'Undo now')).toBeUndefined();
            expect(fake.writes).toHaveLength(0);
            unmount();
        }
    });

    it('an Undo refused by the server shows the fixed sentence and keeps the confirm', async () => {
        await openPiece(46, { write: () => refuse(409, 'stale'), read: (u) => (u.endsWith('/pieces/t46') ? json(detailOf('t46')) : json(previewOf())) });
        await vi.waitFor(() => expect(btn(dialog(), 'Undo this change')).toBeDefined());
        await userEvent.click(btn(dialog(), 'Undo this change'));
        await vi.waitFor(() => expect(btn(dialog(), 'Undo now')).toBeDefined());
        await userEvent.click(btn(dialog(), 'Undo now'));
        await vi.waitFor(() => expect(txt(dialog().querySelector('[data-undo]'))).toContain(table['refusal.stale']));
    });
});
