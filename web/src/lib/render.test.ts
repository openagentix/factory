import { fireEvent } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { FIXTURES, KEY, clone, fakeEnv, mount, type Json } from './testkit';
import { render } from '@testing-library/svelte';
import Shell from './Shell.svelte';

const text = (e: Element) => e.textContent ?? '';
const bays = (c: HTMLElement) => [...c.querySelectorAll<HTMLElement>('.bay')];

describe('Floor: the line', () => {
    it('draws seven bays in order with the right counts', async () => {
        const { container } = await mount(FIXTURES.demo);
        const b = bays(container);
        expect(b.map((x) => x.dataset.station)).toEqual(['1', '2', '3', '4', '5', '6', '7']);
        const expected = [1, 3, 3, 1, 2, 3, 4];
        b.forEach((x, i) => {
            expect(text(x.querySelector('.cnt')!)).toMatch(new RegExp(`^${expected[i]} (pieces?|today)$`));
            expect(x.querySelectorAll('button.tag').length).toBe(expected[i]);
        });
        expect(text(b[0]!.querySelector('.nm')!)).toContain('Plan');
        expect(text(b[6]!.querySelector('.nm')!)).toContain('Accepted');
    });

    it('draws a stuck bay with the stripes class and the word', async () => {
        const { container } = await mount(FIXTURES.demo);
        const check = bays(container)[3]!;
        expect(check.className).toContain('s-stuck');
        expect(text(check.querySelector('.why')!)).toContain('Ticket 57: the checks failed. Try 2 of 3.');
        expect(text(check.querySelector('.tag .s')!)).toContain('Stuck');
    });

    it('never shows state by colour alone: every state chip and tag state has an icon and a word', async () => {
        const { container } = await mount(FIXTURES.demo);
        const marks = [...container.querySelectorAll('.tag .s, .chip, .why, .legend span')];
        expect(marks.length).toBeGreaterThan(20);
        for (const m of marks) {
            expect(m.querySelector('svg'), text(m)).not.toBeNull();
            expect(text(m).trim().length, 'a word').toBeGreaterThan(2);
        }
    });

    it('tells the bays that wait for a machine, for a person, and that work, apart by words', async () => {
        const { container } = await mount(FIXTURES.demo);
        const t = text(container);
        for (const w of ['Working', 'Waiting for a machine', 'Waiting for a person', 'Stuck', 'Ready']) expect(t).toContain(w);
        expect(text(bays(container)[4]!.querySelector('.stn')!)).toBe('scripted');
    });

    it('shows a claim by another machine as claimed, never with a step', async () => {
        const { container } = await mount(FIXTURES.demo);
        const tag = [...container.querySelectorAll('.tag')].find((t) => text(t).includes('#60'))!;
        expect(text(tag)).toContain('laptop-b');
        expect(text(tag)).toContain('Claimed by another machine');
    });

    it('computes ages from the server time, not the browser clock', async () => {
        const { container } = await mount(FIXTURES.demo);
        // The snapshot is made at 12:00 UTC on 2026-11-27. Plan of goal 44 has waited since 10:25.
        const plan = [...container.querySelectorAll('.tag')].find((t) => text(t).includes('Plan, goal 44'))!;
        expect(text(plan)).toContain('1 h 35 min');
    });

    it('moves the belt only when something is moving', async () => {
        const a = await mount(FIXTURES.demo);
        expect(a.container.querySelector('.belt')!.className).not.toContain('halt');
        a.unmount();
        const b = await mount(FIXTURES.paused);
        expect(b.container.querySelector('.belt')!.className).toContain('halt');
    });
});

describe('Floor: honest states', () => {
    it('the everything-unknown snapshot shows no number in any gauge, no piece, and the unknown lamp word', async () => {
        const { container } = await mount(FIXTURES.unknown);
        const g = [...container.querySelectorAll('.gauges .g')];
        expect(g).toHaveLength(5);
        for (const x of g) {
            expect(text(x.querySelector('.v')!)).toBe('Could not read');
            expect(text(x)).not.toMatch(/[0-9]/);
        }
        expect(container.querySelectorAll('.tag')).toHaveLength(0);
        expect(text(container.querySelector('.say .st')!)).toBe('Could not read');
        expect(container.querySelectorAll('.lamp.on')).toHaveLength(0);
        for (const b of bays(container)) expect(b.className).toContain('s-unknown');
        for (const c of container.querySelectorAll('.bay .cnt')) expect(text(c).trim()).toBe('');
        expect(text(container)).toContain('Could not read');
        expect(text(container)).not.toContain('Running');
        expect(text(container)).not.toContain('DEMO');
        // mode unknown never claims demo or live
        expect(text(container.querySelector('.band')!)).toContain('Could not read');
    });

    it('the nothing-built snapshot says Not built yet, with no count and no green', async () => {
        const { container } = await mount(FIXTURES.notbuilt);
        for (const b of bays(container)) {
            expect(b.className).toContain('s-notbuilt');
            expect(text(b.querySelector('.why')!)).toBe('Not built yet');
            expect(text(b.querySelector('.cnt')!).trim()).toBe('');
        }
        for (const g of container.querySelectorAll('.gauges .v')) expect(text(g)).toBe('Not built yet');
        expect(container.querySelector('.lamp.g.on')).toBeNull();
        expect(text(container.querySelector('.say .st')!)).toBe('Could not read');
    });

    it('a paused line shows the Paused lamp word and the fixed phrase', async () => {
        const { container } = await mount(FIXTURES.paused);
        expect(text(container.querySelector('.say .st')!)).toBe('Paused');
        expect(text(container)).toContain('The line is paused on this machine.');
        expect(container.querySelector('.lamp.on')).toBeNull();
    });

    it('shows the gauges as a value, Not enough yet, or Not built yet', async () => {
        const { container } = await mount(FIXTURES.demo);
        const v = [...container.querySelectorAll('.gauges .v')].map(text);
        expect(v[0]).toMatch(/^13/);
        expect(v[1]).toMatch(/^4/);
        expect(v[2]).toBe('Not enough yet');
        expect(v[3]).toBe('Not enough yet');
        expect(v[4]).toBe('Not built yet');
    });

    it('draws a reason phrase for every kind of waiting piece', async () => {
        const { container } = await mount(FIXTURES.demo);
        const t = text(container);
        expect(t).toContain('Plan of goal 44: the plan waits for you.');
        expect(t).toContain('Ticket 58: the agent asked a question.');
        expect(t).toContain('Ticket 55: a change needs your yes or no.');
        expect(t).toContain('Ticket 61: waiting for Checks of goal 43.');
    });
});

describe('demo marking', () => {
    it('shows the DEMO plate, band and title only in demo mode', async () => {
        const d = await mount(FIXTURES.demo);
        expect(d.container.querySelector('.demoplate')).not.toBeNull();
        expect(text(d.container.querySelector('.band')!)).toBe('DEMO. A made-up project on a fake GitHub. The factory code that moves this line is real, except stations marked scripted.');
        await vi.waitFor(() => expect(document.title.startsWith('DEMO')).toBe(true));
        expect(text(d.container.querySelector('.repo')!)).toContain('demo/booking (fake)');
        d.unmount();
        const p = await mount(FIXTURES.paused);
        expect(p.container.querySelector('.demoplate')).toBeNull();
        expect(p.container.querySelector('.band')).toBeNull();
        await vi.waitFor(() => expect(document.title).toBe('Factory Floor'));
    });

    it('puts the band on every screen in demo mode', async () => {
        for (const r of ['floor', 'needs-you', 'goals', 'live', 'team', 'runs', 'autonomy', 'plant', 'record']) {
            const m = await mount(FIXTURES.demo, r);
            expect(text(m.container.querySelector('.band')!), r).toContain('DEMO.');
            m.unmount();
        }
    });
});

describe('screens', () => {
    it('Needs you lists every call with its actions disabled and Not built yet in view', async () => {
        const { container } = await mount(FIXTURES.demo, 'needs-you');
        expect(container.querySelectorAll('.call')).toHaveLength(4);
        const buttons = [...container.querySelectorAll<HTMLButtonElement>('.call button')];
        expect(buttons.map(text)).toEqual(expect.arrayContaining(['Approve the plan', 'Keep planning', 'Send answer', 'Accept', 'Try it', 'Undo a change', 'Yes, that is what I want', 'No, send it back']));
        for (const b of buttons) {
            expect(b.disabled).toBe(true);
            expect(text(b.parentElement!)).toContain('Not built yet');
        }
        expect(container.querySelector<HTMLTextAreaElement>('textarea')!.disabled).toBe(true);
    });

    it('offers Accept once, for the whole holding area, listing each goal', async () => {
        const { container } = await mount(FIXTURES.demo, 'needs-you');
        const accepts = [...container.querySelectorAll('button')].filter((b) => text(b) === 'Accept');
        expect(accepts).toHaveLength(1);
        expect(text(container.querySelector('.acc-goals')!)).toContain('Goal 41');
        expect(text(container)).toContain('3 changes are in the holding area. Accept them all, or ask a developer to finish this step.');
    });

    it('puts the agent question in a quoted box that says who wrote it', async () => {
        const { container } = await mount(FIXTURES.demo, 'needs-you');
        const q = container.querySelector('.quote')!;
        expect(text(q)).toContain('Written by the agent');
        expect(text(q)).toContain('After how long may another machine take it');
    });

    it('Goals shows every goal with its lines in their own list, and a Plan a new goal button that is not built', async () => {
        const { container } = await mount(FIXTURES.demo, 'goals');
        expect(container.querySelectorAll('main .box')).toHaveLength(4);
        expect(container.querySelectorAll('ul.lines li').length).toBe(4 + 4 + 2 + 3);
        expect(text(container.querySelector('.linesnote')!)).toContain("person's own words");
        const plan = [...container.querySelectorAll('button')].find((b) => text(b) === 'Plan a new goal')!;
        expect(plan.disabled).toBe(true);
        expect(text(container)).toContain('Ticket 57: the checks failed. Try 2 of 3.');
        expect(text(container)).toContain('Ticket 58: the agent asked a question.');
    });

    it('goal lanes open and close their lines', async () => {
        const { container } = await mount(FIXTURES.demo);
        const btn = [...container.querySelectorAll('button')].find((b) => text(b) === 'Show lines')!;
        expect(container.querySelectorAll('.lane .lines')).toHaveLength(0);
        await fireEvent.click(btn);
        expect(container.querySelectorAll('.lane .lines')).toHaveLength(1);
        expect(btn.getAttribute('aria-expanded')).toBe('true');
        expect(text(btn)).toBe('Hide lines');
    });

    it.each([
        ['live', 'Not built yet'], ['team', 'Not built yet'], ['runs', 'Not built yet'], ['autonomy', 'Not built yet'],
        ['plant', 'Designed, not built'], ['record', 'Designed, not built'],
    ])('%s shows its heading, one paragraph, the status, and no number', async (route, status) => {
        const { container } = await mount(FIXTURES.demo, route);
        const main = container.querySelector('main')!;
        expect(main.querySelector('h1')).not.toBeNull();
        expect(text(main.querySelector('.nobuilt')!)).toContain(status);
        expect(main.querySelectorAll('table, .tag, .gauges')).toHaveLength(0);
        // The demo band mentions no number either.
        expect(text(main)).not.toMatch(/[0-9]/);
    });

    it('uses the one name Pause the line, disabled, and not Stop the line', async () => {
        const { container } = await mount(FIXTURES.demo);
        const b = [...container.querySelectorAll('button')].find((x) => text(x) === 'Pause the line')!;
        expect(b.disabled).toBe(true);
        expect(text(container)).not.toContain('Stop the line');
    });

    it('routes by plain #name and marks the current screen', async () => {
        const m = await mount(FIXTURES.demo);
        const nav = [...m.container.querySelectorAll<HTMLButtonElement>('.nav')];
        expect(nav).toHaveLength(9);
        expect(nav.map((n) => text(n).replace(/[0-9]/g, '').trim())).toEqual(['Floor', 'Needs you', 'Goals', 'Live', 'Team', 'Runs and changes', 'Autonomy', 'Plant', 'Record']);
        await fireEvent.click(nav[2]!);
        expect(m.fake.env.location.hash).toBe('goals');
        expect(nav[2]!.getAttribute('aria-current')).toBe('page');
        expect(nav[0]!.getAttribute('aria-current')).toBeNull();
        expect(text(m.container.querySelector('h1')!)).toBe('Goals');
    });

    it('toggles the theme and remembers it', async () => {
        const m = await mount(FIXTURES.demo);
        await fireEvent.click([...m.container.querySelectorAll('button')].find((b) => text(b).includes('Light / dark'))!);
        const t = document.documentElement.getAttribute('data-theme');
        expect(t === 'dark' || t === 'light').toBe(true);
        expect(m.fake.env.localStorage.getItem('oxf-theme')).toBe(t);
        document.documentElement.removeAttribute('data-theme');
    });
});

describe('untrusted text', () => {
    it('shows hostile titles as text and makes no element from them', async () => {
        const f = clone(FIXTURES.demo) as unknown as Json & { pieces: { title: { text: string } }[]; goals: { title: { text: string }; lines: { text: { text: string } }[] }[]; calls: { question: { text: string } }[] };
        f.pieces[0]!.title.text = '<script>window.hacked=1</script>';
        f.pieces[1]!.title.text = '<img src=x onerror="window.hacked=2">';
        f.pieces[2]!.title.text = 'a‮evil⁦b';
        f.goals[0]!.title.text = '<b>bold</b><iframe src="x"></iframe>';
        f.goals[0]!.lines[0]!.text.text = '<a href="javascript:1">x</a>';
        f.calls.find((c) => c.question)!.question.text = '<style>*{display:none}</style>';
        const m = await mount(f, 'floor');
        await fireEvent.click([...m.container.querySelectorAll('button')].find((b) => text(b) === 'Show lines')!);
        const c = m.container;
        expect(c.querySelector('script')).toBeNull();
        expect(c.querySelector('main img')).toBeNull();
        expect(c.querySelector('main iframe')).toBeNull();
        expect(c.querySelector('main a')).toBeNull();
        expect(c.querySelector('main style')).toBeNull();
        expect(c.querySelector('main b')?.textContent ?? '').not.toBe('bold');
        expect(text(c)).toContain('<script>window.hacked=1</script>');
        expect(text(c)).toContain('<img src=x onerror="window.hacked=2">');
        expect(text(c)).toContain('a‮evil⁦b');
        expect(text(c)).toContain('<b>bold</b><iframe src="x"></iframe>');
        expect((window as unknown as { hacked?: number }).hacked).toBeUndefined();
        // A title sits in its own element, never inside a sentence.
        for (const t of c.querySelectorAll('.tag .t')) expect(t.children.length).toBe(0);
    });

    it('a hostile string in a number slot makes the sentence read Could not read, not markup', async () => {
        const f = clone(FIXTURES.demo) as unknown as { pieces: { id: string; reason: { code: string; attempt: number | string } | null }[]; log: { goal?: unknown; kind: string }[] };
        const p = f.pieces.find((x) => x.id === 't57')!;
        p.reason!.attempt = '<img src=x>';
        f.log[0] = { journalSeq: 99, time: null, kind: 'accepted', goal: '<script>' };
        const m = await mount(f);
        expect(m.container.querySelector('main img')).toBeNull();
        expect(text(m.container)).not.toContain('<img src=x>');
        expect(text(m.container)).toContain('One entry could not be read.');
    });
});

describe('forbidden words', () => {
    const FORBIDDEN = /\b(pull requests?|merg\w*|diffs?|review\w*|branch\w*|commit\w*|staging|gates?|harness\w*|run ids?|tokens?|evidence)\b/i;
    for (const [name, fx] of Object.entries(FIXTURES)) {
        it(`never appear on Floor, Needs you or Goals (${name})`, async () => {
            for (const r of ['floor', 'needs-you', 'goals']) {
                const m = await mount(fx, r);
                m.container.querySelectorAll('button').forEach((b) => {
                    if (text(b) === 'Show lines') b.click();
                });
                await vi.waitFor(() => expect(m.container.querySelector('main')).not.toBeNull());
                expect(text(m.container).match(FORBIDDEN)?.[0], `${name} ${r}`).toBeUndefined();
                m.unmount();
            }
        });
    }
});

describe('keyboard and the drawer', () => {
    it('makes every tag a button', async () => {
        const { container } = await mount(FIXTURES.demo);
        const tags = [...container.querySelectorAll('.tag')];
        expect(tags.length).toBe(17);
        for (const t of tags) expect(t.tagName).toBe('BUTTON');
    });

    it('opens, traps focus, closes on Escape and returns focus', async () => {
        const user = userEvent.setup();
        const { container } = await mount(FIXTURES.demo);
        const tag = [...container.querySelectorAll<HTMLButtonElement>('.tag')].find((t) => text(t).includes('#57'))!;
        tag.focus();
        await user.keyboard('{Enter}');
        const dlg = await vi.waitFor(() => {
            const d = document.querySelector('[role="dialog"]');
            if (!d) throw new Error('no dialog');
            return d as HTMLElement;
        });
        expect(dlg.getAttribute('aria-modal')).toBe('true');
        expect(dlg.contains(document.activeElement)).toBe(true);
        expect(text(dlg.querySelector('h2')!)).toBe('Lowest claim wins');
        expect(text(dlg)).toContain('Ticket 57: the checks failed. Try 2 of 3.');
        expect(text(dlg)).toContain('Not built yet');
        for (let i = 0; i < 5; i++) {
            await user.tab();
            expect(dlg.contains(document.activeElement), `tab ${i}`).toBe(true);
        }
        await user.tab({ shift: true });
        expect(dlg.contains(document.activeElement)).toBe(true);
        await user.keyboard('{Escape}');
        await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).toBeNull());
        await vi.waitFor(() => expect(document.activeElement).toBe(tag));
    });

    it('says Not built yet for route, decided for you and could not be confirmed', async () => {
        const { container } = await mount(FIXTURES.demo);
        await fireEvent.click(container.querySelector('.tag')!);
        const dlg = await vi.waitFor(() => document.querySelector('[role="dialog"]') as HTMLElement);
        const heads = [...dlg.querySelectorAll('h3')].map(text);
        expect(heads).toEqual(['Its route so far', 'Decided for you', 'Could not be confirmed']);
        expect(dlg.textContent!.match(/Not built yet/g)!.length).toBe(3);
    });
});

describe('the key and the data', () => {
    it('shows the no-key screen when there is no key, and reads nothing', async () => {
        const fake = fakeEnv({ hash: '' });
        const { container } = render(Shell, { props: { env: fake.env } });
        await vi.waitFor(() => expect(text(container)).toContain('This page needs the link printed when the program started.'));
        expect(fake.calls).toHaveLength(0);
        expect(container.querySelector('.rail')).toBeNull();
    });

    it('takes the key from the fragment, keeps it in session storage, clears the address bar, and sends it only as a header', async () => {
        const fake = fakeEnv({ hash: '#t=' + KEY });
        const { container } = render(Shell, { props: { env: fake.env } });
        await vi.waitFor(() => expect(container.querySelector('.bay')).not.toBeNull());
        expect(fake.store.get('oxf-key')).toBe(KEY);
        expect(fake.urls).toEqual(['/']);
        expect(fake.calls.length).toBeGreaterThanOrEqual(2);
        for (const c of fake.calls) {
            expect(c.url).not.toContain(KEY);
            expect(c.url).not.toContain('?');
            expect(c.headers.Authorization).toBe('Bearer ' + KEY);
        }
        expect(JSON.stringify([...fake.store])).not.toContain('"oxf-theme"');
    });

    it('shows the no-key screen on a 401', async () => {
        const fake = fakeEnv({ hash: '#floor', stored: KEY, snapshot: 401 });
        const { container } = render(Shell, { props: { env: fake.env } });
        await vi.waitFor(() => expect(text(container)).toContain('This page needs the link printed when the program started.'));
    });

    it('shows Could not read, not data, when the first read fails', async () => {
        const fake = fakeEnv({ hash: '#floor', stored: KEY, snapshot: 500 });
        const { container } = render(Shell, { props: { env: fake.env } });
        await vi.waitFor(() => expect(text(container)).toContain('The factory did not answer'));
        expect(container.querySelectorAll('.tag, .gauges')).toHaveLength(0);
    });

    it('after a failed read it keeps the last snapshot as old: unknown lamp, a band and the time it is as of', async () => {
        const m = await mount(FIXTURES.demo);
        expect(text(m.container.querySelector('.say .st')!)).toBe('Stuck');
        m.fake.setSnapshot(500);
        m.fake.event('floor');
        await vi.waitFor(() => expect(m.container.querySelector('.band.warn')).not.toBeNull(), { timeout: 3000 });
        expect(text(m.container.querySelector('.say .st')!)).toBe('Could not read');
        expect(text(m.container.querySelector('.say')!)).toMatch(/as of \d/);
        expect(m.container.querySelector('.lamp.on')).toBeNull();
        expect(m.container.querySelector('.linewrap')!.className).toContain('stale');
        expect(m.container.querySelector('.belt')!.className).toContain('halt');
        // and it recovers by itself
        m.fake.setSnapshot(FIXTURES.demo);
        m.fake.event('floor');
        await vi.waitFor(() => expect(m.container.querySelector('.band.warn')).toBeNull(), { timeout: 3000 });
        expect(text(m.container.querySelector('.say .st')!)).toBe('Stuck');
    });

    it('animates a tag only when its station changed between two snapshots', async () => {
        const m = await mount(FIXTURES.demo);
        expect(m.container.querySelectorAll('.tag.fresh')).toHaveLength(0);
        const next = clone(FIXTURES.demo) as unknown as { pieces: { id: string; station: number }[] };
        next.pieces.find((p) => p.id === 't61')!.station = 3;
        m.fake.setSnapshot(next);
        m.fake.event('floor');
        await vi.waitFor(() => expect(m.container.querySelectorAll('.tag.fresh')).toHaveLength(1), { timeout: 3000 });
        expect(text(m.container.querySelector('.tag.fresh')!)).toContain('#61');
    });

    it('announces once per change of signal, not once per tag', async () => {
        const m = await mount(FIXTURES.demo);
        const live = m.container.querySelector('[role="status"]')!;
        expect(live.getAttribute('aria-live')).toBe('polite');
        const first = text(live);
        expect(first).toContain('Stuck.');
        const next = clone(FIXTURES.demo) as unknown as { pieces: { id: string; station: number }[] };
        next.pieces.find((p) => p.id === 't61')!.station = 3;
        m.fake.setSnapshot(next);
        m.fake.event('floor');
        await vi.waitFor(() => expect(m.container.querySelector('.tag.fresh')).not.toBeNull(), { timeout: 3000 });
        expect(text(live)).toBe(first);
    });
});

describe('source files', () => {
    it('never use {@html', async () => {
        const { readdirSync, readFileSync, statSync } = await import('node:fs');
        const { join } = await import('node:path');
        const walk = (d: string): string[] => readdirSync(d).flatMap((n) => (statSync(join(d, n)).isDirectory() ? walk(join(d, n)) : [join(d, n)]));
        const root = join(process.cwd(), 'src');
        const files = walk(root).filter((f) => /\.(svelte|ts|js|html)$/.test(f) && !f.endsWith('render.test.ts'));
        expect(files.length).toBeGreaterThan(20);
        for (const f of files) expect(readFileSync(f, 'utf8'), f).not.toContain('{@html');
    });
});
