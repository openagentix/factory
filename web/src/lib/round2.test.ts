// Fix round 2: the same lamp rule as the Go side, counts that match the pieces, every stale mark, gauge ranges,
// a fragment that holds t= anywhere, and data the Go side really sends.
import { fireEvent } from '@testing-library/svelte';
import { describe, expect, it, vi } from 'vitest';
import demo from '../fixtures/floor.demo.json';
import notbuilt from '../fixtures/floor.notbuilt.json';
import paused from '../fixtures/floor.paused.json';
import unknown from '../fixtures/floor.unknown.json';
import { readKeys, type KeyEnv } from './key';
import { parseFloor } from './parse';
import { FIXTURES, clone, mount } from './testkit';
import { gaugeView } from './view';

type Any = any;
const txt = (e: Element | null) => (e?.textContent ?? '').replace(/\s+/g, ' ').trim();

// B. The Go lamp rule is in internal/floor/project.go, buildSignal. Each case breaks one part of it.
describe('B. the lamp follows the Go rule, and the counts match the pieces', () => {
    const rows: [string, (f: Any) => void][] = [
        ['green+running but stuck=1, waiting=3, calls=3', (f) => { f.signal.lamp = 'green'; f.signal.word = 'running'; }],
        ['off+idle but moving=2 and stuck=1', (f) => { f.signal.lamp = 'off'; f.signal.word = 'idle'; }],
        ['amber with no calls and waiting=0', (f) => { f.calls = []; f.pieces.forEach((p: Any) => { if (p.state === 'waiting_person') p.state = 'ready'; }); f.signal.inputs.waitingForPerson = 0; f.signal.inputs.stuck = 0; f.signal.inputs.oldestStuck = null; f.pieces.forEach((p: Any) => { if (p.state === 'stuck') p.state = 'ready'; }); f.stations.forEach((s: Any) => (s.oldestStuck = null)); f.signal.lamp = 'amber'; f.signal.word = 'needs_a_person'; }],
        ['moving=9 but no piece is working', (f) => { f.pieces.forEach((p: Any) => { if (p.state === 'working') p.state = 'ready'; }); f.signal.inputs.moving = 9; }],
        ['moving=0 but pieces are working here', (f) => (f.signal.inputs.moving = 0)],
        ['stuck=0 but a stuck root exists', (f) => (f.signal.inputs.stuck = 0)],
        ['stuck=7 but one stuck root', (f) => (f.signal.inputs.stuck = 7)],
        ['waiting=0 but pieces wait', (f) => (f.signal.inputs.waitingForPerson = 0)],
        ['movingElsewhere wrong', (f) => (f.signal.inputs.movingElsewhere = 5)],
        ['inHoldingArea wrong', (f) => (f.signal.inputs.inHoldingArea = 9)],
        ['oldestStuck is not the oldest', (f) => { f.signal.inputs.oldestStuck.since = '2026-11-27T11:59:00Z'; }],
        ['red but nothing stuck', (f) => { f.pieces.forEach((p: Any) => { if (p.state === 'stuck') p.state = 'ready'; }); f.signal.inputs.stuck = 0; f.signal.inputs.oldestStuck = null; f.stations.forEach((s: Any) => (s.oldestStuck = null)); }],
        ['amber word on a red line', (f) => { f.signal.lamp = 'amber'; f.signal.word = 'needs_a_person'; }],
        ['unknown lamp with no unknown source', (f) => { f.signal.lamp = 'unknown'; f.signal.word = 'could_not_read'; }],
    ];
    for (const [name, mut] of rows) {
        it(name, () => {
            const f = clone(demo) as Any;
            mut(f);
            expect(parseFloor(f)).toBeNull();
        });
    }
    it('paused word without paused, and idle with moving pieces', () => {
        const f = clone(paused) as Any;
        f.signal.inputs.paused = false;
        expect(parseFloor(f)).toBeNull();
        const g = clone(paused) as Any;
        g.signal.word = 'idle';
        expect(parseFloor(g)).toBeNull(); // paused is true: the rule says paused
    });
    it('accepts every fixture and the Go-legal edge cases', () => {
        for (const x of [demo, notbuilt, paused]) expect(parseFloor(clone(x))).not.toBeNull();
        const u = clone(unknown) as Any;
        u.generatedAt = '2026-11-27T12:00:00Z';
        expect(parseFloor(u)).not.toBeNull();
        const cases: [string, (f: Any) => void][] = [
            ['piece since null', (f) => (f.pieces[0].since = null)],
            ['hostTime null', (f) => (f.hostTime = null)],
            ['count null with source read_failed', (f) => { f.stations[3].count = null; f.sources.gate = 'read_failed'; }],
            ['goal lines null', (f) => { f.goals[0].lines = null; f.goals[0].course = null; f.goals[0].linesAsOf = null; }],
            ['call question null', (f) => { const c = f.calls.find((x: Any) => x.kind === 'question'); c.question = null; }],
            ['unreadable journal entry', (f) => (f.log[0] = { journalSeq: f.log[0].journalSeq, time: f.log[0].time, kind: 'unreadable' })],
            ['gauge not built', (f) => (f.gauges[3] = { key: 'goal_to_holding', state: 'not_built', value: null, samples: null, window: '7d' })],
            ['piece since 59 s after the snapshot', (f) => (f.pieces[0].since = '2026-11-27T12:00:59Z')],
            ['50 log entries', (f) => { while (f.log.length < 50) f.log.push({ ...clone(f.log[0]), journalSeq: 1000 + f.log.length }); }],
            ['a title of 120 astral characters', (f) => (f.pieces[0].title.text = '😀'.repeat(120))],
            // E. Data the Go side can really send.
            ['demo story empty', (f) => (f.demo.story = '')],
            ['fact number 2^31 (maxFactNumber in internal/floor/fact.go)', (f) => { const c = f.calls.find((x: Any) => Object.keys(x.facts).length); c.facts[Object.keys(c.facts)[0]!] = 2 ** 31; }],
            ['station 07 count 5000', (f) => (f.stations[6].count = 5000)],
            ['call goal missing from goals', (f) => (f.calls.find((x: Any) => x.goal !== null).goal = 9999)],
        ];
        for (const [n, mut] of cases) {
            const f = clone(demo) as Any;
            mut(f);
            expect(parseFloor(f), n).not.toBeNull();
        }
    });
    it('still rejects the hostile cases near the relaxed ones', () => {
        const cases: [string, (f: Any) => void][] = [
            ['demo story with markup', (f) => (f.demo.story = '<b>')],
            ['fact number above 2^31', (f) => { const c = f.calls.find((x: Any) => Object.keys(x.facts).length); c.facts[Object.keys(c.facts)[0]!] = 2 ** 31 + 1; }],
            ['fact number negative', (f) => { const c = f.calls.find((x: Any) => Object.keys(x.facts).length); c.facts[Object.keys(c.facts)[0]!] = -1; }],
            ['station count absurd', (f) => (f.stations[6].count = 1e12)],
            ['call goal zero', (f) => (f.calls.find((x: Any) => x.goal !== null).goal = 0)],
            ['call goal string', (f) => (f.calls.find((x: Any) => x.goal !== null).goal = '9')],
            ['call goal negative', (f) => (f.calls.find((x: Any) => x.goal !== null).goal = -1)],
        ];
        for (const [n, mut] of cases) {
            const f = clone(demo) as Any;
            mut(f);
            expect(parseFloor(f), n).toBeNull();
        }
    });
});

// C. A gauge value out of range reads Could not read. The snapshot is not rejected for it.
describe('C. gauge ranges per key', () => {
    const g = (key: string, value: number) => gaugeView({ key, state: 'ok', value, samples: null, window: 'now' } as Any);
    it('has a range per key', () => {
        expect(g('in_work', 1_000_001).honest).toBe('Could not read');
        expect(g('accepted_today', 1_000_001).honest).toBe('Could not read');
        expect(g('right_first_time', 101).honest).toBe('Could not read');
        expect(g('right_first_time', 100).value).toBe('100%');
        expect(g('goal_to_holding', 315_576_001).honest).toBe('Could not read');
        expect(g('goal_to_holding', 315_576_000).honest).toBeNull();
        expect(g('attention', 1_000_001).honest).toBe('Could not read');
        expect(g('attention', 250).value).toBe('2.5');
    });
    it('a bad gauge value does not reject the snapshot', () => {
        const f = clone(demo) as Any;
        f.gauges[2].state = 'ok';
        f.gauges[2].value = 900;
        expect(parseFloor(f)).not.toBeNull();
    });
});

// D. A fragment with t= anywhere leaves the address bar at once.
describe('D. a fragment with t= anywhere is cleared', () => {
    for (const hash of ['#goals&t=KEYKEYKEYKEYKEYKEYKEY', '#x=1&t=KEYKEYKEYKEYKEYKEYKEY', '#t=short', '#t=']) {
        it(hash, () => {
            const urls: string[] = [];
            const e: KeyEnv = {
                location: { hash, pathname: '/', search: '' },
                history: { replaceState: (_d, _u, url) => void urls.push(String(url)) },
                sessionStorage: { getItem: () => null, setItem: () => {} },
            };
            expect(readKeys(e).candidate).toBeNull();
            expect(urls).toEqual(['/']);
        });
    }
});

// A. In the failed state every place that shows snapshot data carries the stale mark.
describe('A. the failed state marks every place that shows the last snapshot', () => {
    it('walks the whole document, drawer and rail included', async () => {
        const m = await mount(FIXTURES.demo);
        await fireEvent.click([...m.container.querySelectorAll('.tag')].find((t) => txt(t).includes('#57'))!);
        await vi.waitFor(() => expect(document.querySelector('[role="dialog"]')).not.toBeNull());
        expect(txt(document.querySelector('.rail .n'))).toBe('4');
        m.fake.setSnapshot(500);
        m.fake.event('floor');
        await vi.waitFor(() => expect(m.container.querySelector('[role=alert]')).not.toBeNull(), { timeout: 4000 });
        const dlg = document.querySelector('[role="dialog"]')!;
        expect(txt(dlg)).toMatch(/as of \d/);
        expect(dlg.closest('.stale') ?? (dlg.classList.contains('stale') ? dlg : null)).not.toBeNull();
        expect(document.querySelector('.rail .n')).toBeNull(); // the count is hidden
        expect(document.title).not.toMatch(/\d/);
        // Every element that shows data of the snapshot sits under a stale-marked ancestor.
        const f = demo as Any;
        const words = [...f.pieces.map((p: Any) => p.title.text), ...f.goals.map((g: Any) => g.title.text), 'demo/booking', 'DEMO.', 'Demo'].filter((w: string) => w.length > 3);
        const bad: string[] = [];
        for (const el of document.body.querySelectorAll('*')) {
            if (el.children.length > 0 && ![...el.childNodes].some((n) => n.nodeType === 3 && (n.textContent ?? '').trim())) continue;
            const own = [...el.childNodes].filter((n) => n.nodeType === 3).map((n) => n.textContent).join('');
            if (words.some((w) => own.includes(w)) && !el.closest('.stale')) bad.push(own.trim().slice(0, 50));
        }
        expect(bad).toEqual([]);
        for (const sel of ['.demoplate', '.band', '.repo']) for (const el of document.querySelectorAll(sel)) {
            if (el.getAttribute('role') === 'alert') continue;
            expect(el.closest('.stale'), sel).not.toBeNull();
        }
    });
});
