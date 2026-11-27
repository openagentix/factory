import { describe, expect, it } from 'vitest';
import demo from '../fixtures/floor.demo.json';
import notbuilt from '../fixtures/floor.notbuilt.json';
import paused from '../fixtures/floor.paused.json';
import unknown from '../fixtures/floor.unknown.json';
import type { Floor, Gauge } from './floor';
import { gaugeView, signalView, sparkPoints } from './view';

const f = (x: unknown) => x as Floor;
const NOW = Date.parse('2026-11-27T12:00:00Z');

describe('the signal sentence', () => {
    it('is built from fixed phrases and numbers, and names the oldest stuck piece by its age and station', () => {
        const v = signalView(f(demo), NOW, false);
        expect(v.lamp).toBe('red');
        expect(v.word).toBe('stuck');
        expect(v.parts.map((p) => p.text)).toEqual([
            '2 moving.',
            '1 claimed by another machine.',
            '1 stuck for 40 min at Check.',
            '3 things wait for a person.',
            '3 in the holding area, ready for you to accept.',
        ]);
    });
    it('uses the singular forms', () => {
        const x = structuredClone(demo) as unknown as Floor;
        x.signal.inputs.waitingForPerson = 1;
        x.signal.inputs.moving = 1;
        const t = signalView(x, NOW, false).parts.map((p) => p.text);
        expect(t).toContain('1 moving.');
        expect(t).toContain('1 thing waits for a person.');
    });
    it('shows no number and no green for unknown, not-built and stale data', () => {
        for (const x of [unknown, notbuilt]) {
            const v = signalView(f(x), NOW, false);
            expect(v.lamp).toBe('unknown');
            expect(v.word).toBe('could_not_read');
            expect(v.tone).toBe('unk');
            expect(v.parts.map((p) => p.text).join(' ')).not.toMatch(/[0-9]/);
        }
        const s = signalView(f(demo), NOW, true);
        expect(s.lamp).toBe('unknown');
        expect(s.parts).toEqual([]);
    });
    it('names what could not be read', () => {
        expect(signalView(f(notbuilt), NOW, false).parts[0]!.text).toBe("The factory could not read the settings, the project host, the factory's records.");
    });
    it('says Paused for a paused line', () => {
        const v = signalView(f(paused), NOW, false);
        expect(v.word).toBe('paused');
        expect(v.lamp).toBe('off');
    });
});

describe('gauges', () => {
    const g = (x: Partial<Gauge>): Gauge => ({ key: 'in_work', state: 'ok', value: 3, samples: null, window: 'now', ...x });
    it('shows a value only for an ok state with a whole number', () => {
        expect(gaugeView(g({})).value).toBe('3');
        expect(gaugeView(g({ value: 1 })).unit).toBe('piece on the line');
        expect(gaugeView(g({ key: 'right_first_time', value: 71 })).value).toBe('71%');
        expect(gaugeView(g({ key: 'goal_to_holding', value: 2280 })).value).toBe('38');
        expect(gaugeView(g({ key: 'attention', value: 120 })).value).toBe('1.2');
    });
    it('shows the honest words for every other state, and never a number', () => {
        const cases: [Partial<Gauge>, string][] = [
            [{ state: 'not_enough_yet', value: null }, 'Not enough yet'],
            [{ state: 'not_built', value: null }, 'Not built yet'],
            [{ state: 'read_failed', value: null }, 'Could not read'],
            [{ state: 'ok', value: null }, 'Could not read'],
            [{ state: 'ok', value: 1.5 }, 'Could not read'],
            [{ state: 'not_built', value: 7 }, 'Not built yet'],
            [{ state: 'read_failed', value: 7 }, 'Could not read'],
        ];
        for (const [x, word] of cases) {
            const v = gaugeView(g(x));
            expect(v.kind).toBe('honest');
            expect(v.honest).toBe(word);
            expect(v.value).toBeNull();
        }
    });
    it('draws a line only from a series of two or more days', () => {
        expect(sparkPoints([1])).toBeNull();
        expect(sparkPoints([0, 0, 4])!.last).toEqual([120, 4]);
    });
});
