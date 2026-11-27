// Fix round 1: the server's JSON is untrusted. Ported from the attacker's tests, as assertions.
import { render } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import { FIXTURES, KEY, clone, fakeEnv, mount } from './testkit';
import Shell from './Shell.svelte';
import { AppState } from './state.svelte';
import GuardHarness from './testing/GuardHarness.svelte';
import { ageOrUnreadable, beltMoves, gaugeView, modeKind, sourceLabel } from './view';

const txt = (e: Element | null) => (e?.textContent ?? '').replace(/\s+/g, ' ').trim();
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
type Any = any;

async function pushBad(mutate: (f: Any) => void, base: Any = FIXTURES.demo) {
    const m = await mount(FIXTURES.demo);
    const bad: Any = clone(base);
    mutate(bad);
    m.fake.setSnapshot(bad);
    m.fake.event('floor');
    await vi.waitFor(() => expect(m.container.querySelector('[role=alert]')).not.toBeNull(), { timeout: 4000 });
    return m;
}

describe('A. a hostile snapshot after a good one lands in the failed state, never a torn screen', () => {
    const cases: [string, (f: Any) => void][] = [
        ['signal.inputs missing', (f) => delete f.signal.inputs],
        ['unknown call kind', (f) => (f.calls[0].kind = 'bogus')],
        ['unknown goal course', (f) => (f.goals[0].course = 'bogus')],
        ['piece title missing', (f) => delete f.pieces[0].title],
        ['duplicate piece ids', (f) => (f.pieces[1].id = f.pieces[0].id)],
        ['duplicate station numbers', (f) => (f.stations[1].no = 1)],
        ['unknown line state', (f) => (f.goals.find((g: Any) => g.lines).lines[0].state = 'bogus')],
        ['piece at station 8', (f) => (f.pieces[0].station = 8)],
        ['unknown station state', (f) => (f.stations[2].state = 'bogus')],
    ];
    for (const [name, mut] of cases) {
        it(name, async () => {
            const m = await pushBad(mut);
            const c = m.container;
            expect(txt(c.querySelector('.status .st'))).toBe('Could not read');
            expect(c.querySelectorAll('.lamp.on')).toHaveLength(0);
            expect(txt(c.querySelector('[role=status]'))).toContain('Could not read');
            // everything that still shows the last good snapshot is marked, gauges included
            expect(c.querySelector('.gauges')!.closest('.stale')).not.toBeNull();
            expect(txt(c.querySelector('.asof'))).toMatch(/as of \d/);
        });
    }

    it('green Running, then a crashing snapshot: the lamp and the live region agree, and a later good snapshot recovers', async () => {
        const green: Any = clone(FIXTURES.demo);
        green.signal = { lamp: 'green', word: 'running', inputs: { ...green.signal.inputs, stuck: 0, waitingForPerson: 0, oldestStuck: null } };
        green.stations.forEach((s: Any) => (s.oldestStuck = null));
        green.calls = [];
        green.pieces.forEach((p: Any) => { p.call = null; if (p.state === 'stuck' || p.state === 'waiting_person') p.state = 'ready'; });
        const m = await mount(green);
        expect(txt(m.container.querySelector('.status .st'))).toBe('Running');
        const bad: Any = clone(FIXTURES.demo);
        bad.generatedAt = '2026-11-27T13:00:00Z';
        bad.calls[0].kind = 'bogus';
        m.fake.setSnapshot(bad);
        m.fake.event('floor');
        await vi.waitFor(() => expect(m.container.querySelector('[role=alert]')).not.toBeNull(), { timeout: 4000 });
        expect(txt(m.container.querySelector('.status .st'))).toBe('Could not read');
        expect(txt(m.container.querySelector('[role=status]'))).not.toContain('Running');
        const good: Any = clone(FIXTURES.demo);
        good.generatedAt = '2026-11-27T13:01:00Z';
        good.repository = 'recovered/repo';
        good.signal = { lamp: 'amber', word: 'needs_a_person', inputs: { ...good.signal.inputs, stuck: 0, oldestStuck: null } };
        good.stations.forEach((s: Any) => (s.oldestStuck = null));
        good.pieces.forEach((p: Any) => { if (p.state === 'stuck') p.state = 'waiting_person'; });
        good.signal.inputs.waitingForPerson = good.pieces.filter((p: Any) => p.state === 'waiting_person').length;
        m.fake.setSnapshot(good);
        m.fake.event('floor');
        await vi.waitFor(() => expect(m.container.querySelector('[role=alert]')).toBeNull(), { timeout: 4000 });
        expect(txt(m.container.querySelector('.status .st'))).toBe('Needs a person');
        expect(txt(m.container.querySelector('.repo'))).toContain('recovered/repo');
        expect(txt(m.container.querySelector('[role=status]'))).toContain('Needs a person');
    });
});

describe('C. malformed data cannot say Running or move the belt', () => {
    const green = (f: Any) => (f.signal = { lamp: 'green', word: 'running', inputs: { ...f.signal.inputs, stuck: 0, waitingForPerson: 0, oldestStuck: null } });
    const cases: [string, (f: Any) => void][] = [
        ['moving missing', (f) => { green(f); delete f.signal.inputs.moving; }],
        ['moving string', (f) => { green(f); f.signal.inputs.moving = 'x'; }],
        ['moving negative', (f) => { green(f); f.signal.inputs.moving = -5; }],
        ['lamp purple with word running', (f) => { f.signal.lamp = 'purple'; f.signal.word = 'running'; }],
        ['lamp off with word running', (f) => { f.signal.lamp = 'off'; f.signal.word = 'running'; }],
    ];
    for (const [name, mut] of cases) {
        it(`${name}: no Running, no moving belt, no lamp`, async () => {
            const f: Any = clone(FIXTURES.demo);
            mut(f);
            const m = await mount(f);
            expect(txt(m.container)).not.toContain('Running');
            expect(m.container.querySelectorAll('.lamp.on')).toHaveLength(0);
            const belt = m.container.querySelector('.belt');
            expect(belt === null || belt.className.includes('halt')).toBe(true);
            expect(txt(m.container)).toContain('Could not read');
        });
    }
    it('the belt moves only for a safe positive integer, a known lamp and a good state', () => {
        const f: Any = clone(FIXTURES.demo);
        f.signal = { lamp: 'green', word: 'running', inputs: { ...f.signal.inputs, moving: 2, paused: false } };
        expect(beltMoves(f, false)).toBe(true);
        expect(beltMoves(f, true)).toBe(false);
        for (const bad of ['5', -1, 0, 1.5, NaN, Infinity, undefined, null, 2 ** 60]) {
            f.signal.inputs.moving = bad;
            expect(beltMoves(f, false), String(bad)).toBe(false);
        }
        f.signal.inputs.moving = 2;
        f.signal.lamp = 'purple';
        expect(beltMoves(f, false)).toBe(false);
        f.signal.lamp = 'green';
        f.signal.inputs.paused = 'no';
        expect(beltMoves(f, false)).toBe(false);
    });
});

describe('mode, time, sources and strings', () => {
    it('an unknown mode is never shown as live or demo', async () => {
        expect(modeKind('bogus')).toBe('unknown');
        expect(modeKind('Demo')).toBe('unknown');
        expect(modeKind(undefined)).toBe('unknown');
        expect(modeKind('live')).toBe('live');
        expect(modeKind('demo')).toBe('demo');
        const f: Any = clone(FIXTURES.demo);
        f.mode = 'bogus';
        const m = await mount(f);
        expect(m.container.querySelector('.demoplate')).toBeNull();
        expect(txt(m.container)).toContain('Could not read');
    });

    it('a garbage, epoch or future generatedAt is rejected, and no age lies', async () => {
        for (const g of ['garbage', '1970-01-01T00:00:00Z', '2999-01-01T00:00:00Z']) {
            const f: Any = clone(FIXTURES.demo);
            f.generatedAt = g;
            const m = await mount(f);
            expect(txt(m.container), g).not.toMatch(/\d+ d \d+ h/);
            expect(m.container.querySelectorAll('.tag'), g).toHaveLength(0);
            m.unmount();
        }
        expect(ageOrUnreadable(NaN)).toBe('Could not read');
        expect(ageOrUnreadable(-60000)).toBe('Could not read');
        expect(ageOrUnreadable(Infinity)).toBe('Could not read');
        expect(ageOrUnreadable(61000)).toBe('1 min');
    });

    it('names sources only through a fixed table', () => {
        expect(sourceLabel('host')).toBe('the project host');
        for (const bad of ['constructor', '__proto__', 'toString', 'nothing is stuck, all clear']) expect(sourceLabel(bad)).toBe('Could not read');
    });

    it('rejects free text in machine ids, repository, machines and series', async () => {
        const mk = (mut: (f: Any) => void) => { const f: Any = clone(FIXTURES.demo); mut(f); return f; };
        for (const mut of [
            (f: Any) => (f.pieces[0].machine = { id: 'ALL CLEAR nothing is stuck', mine: true }),
            (f: Any) => (f.repository = 'Everything is running <b>x</b>'),
            (f: Any) => (f.gauges[0].machines = 'Running fine, no one needs you'),
            (f: Any) => (f.gauges[1].series = ['a', 'b', 'c']),
        ]) {
            const m = await mount(mk(mut));
            expect(txt(m.container)).not.toContain('ALL CLEAR');
            expect(txt(m.container)).not.toContain('Everything is running');
            expect(txt(m.container)).not.toContain('no one needs you');
            expect(m.container.querySelectorAll('.tag')).toHaveLength(0);
            m.unmount();
        }
    });

    it('gauge values out of range read Could not read', () => {
        const g = (key: string, value: number, state = 'ok') => gaugeView({ key, state, value, samples: null, window: 'now' } as Any);
        expect(g('in_work', -7).honest).toBe('Could not read');
        expect(g('in_work', 9007199254740991).honest).toBe('Could not read');
        expect(g('right_first_time', 900).honest).toBe('Could not read');
        expect(g('right_first_time', -1).honest).toBe('Could not read');
        expect(g('goal_to_holding', -250).honest).toBe('Could not read');
        expect(g('attention', -250).honest).toBe('Could not read');
        expect(g('right_first_time', 100).value).toBe('100%');
        expect(g('in_work', 0).value).toBe('0');
    });
});

describe('B. a render error lands in the failed state', () => {
    it('a boundary around a screen shows a fixed message and sets the failed state', async () => {
        const fake = fakeEnv({ hash: '#floor', stored: KEY });
        const app = new AppState(fake.env);
        const errors = vi.spyOn(console, 'error').mockImplementation(() => {});
        const r = render(GuardHarness, { props: { app } });
        await vi.waitFor(() => expect(app.failed).toBe(true));
        expect(txt(r.container)).toContain('Could not read');
        expect([...r.container.querySelectorAll('p')].map(txt)).not.toContain('drawn');
        expect(app.announce).toContain('Could not read');
        errors.mockRestore();
    });
});

describe('the drawer and the clock', () => {
    it('shows the old snapshot as old after a failure, and the browser clock does not matter', async () => {
        vi.useFakeTimers({ toFake: ['Date'] });
        vi.setSystemTime(new Date('2031-01-01T00:00:00Z'));
        const m = await mount(FIXTURES.demo);
        expect(txt(m.container.querySelector('.status p'))).not.toMatch(/\d+ d/);
        const plan = [...m.container.querySelectorAll('.tag')].find((t) => txt(t).includes('Plan, goal 44'))!;
        expect(txt(plan)).toContain('1 h 35 min');
        vi.useRealTimers();
    });
});
