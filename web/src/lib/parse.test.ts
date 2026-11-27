import { describe, expect, it } from 'vitest';
import demo from '../fixtures/floor.demo.json';
import notbuilt from '../fixtures/floor.notbuilt.json';
import paused from '../fixtures/floor.paused.json';
import unknown from '../fixtures/floor.unknown.json';
import { parseFloor } from './parse';

const c = (): any => structuredClone(demo);

describe('parseFloor accepts the known-good snapshots', () => {
    it('everything unknown, once the server time is known', () => {
        const u: any = structuredClone(unknown);
        expect(parseFloor(u)).toBeNull(); // the server's own clock is unknown: nothing can be judged
        u.generatedAt = '2026-11-27T12:00:00Z';
        expect(parseFloor(u)).not.toBeNull();
    });
    for (const [n, f] of Object.entries({ demo, notbuilt, paused })) {
        it(n, () => expect(parseFloor(structuredClone(f))).not.toBeNull());
    }
});

// One bad input per rule. One failure rejects the whole snapshot.
const BAD: [string, (f: any) => void][] = [
    ['schemaVersion 2', (f) => (f.schemaVersion = 2)],
    ['mode bogus', (f) => (f.mode = 'bogus')],
    ['mode Demo', (f) => (f.mode = 'Demo')],
    ['generatedAt garbage', (f) => (f.generatedAt = 'garbage')],
    ['generatedAt epoch', (f) => (f.generatedAt = '1970-01-01T00:00:00Z')],
    ['generatedAt far future', (f) => (f.generatedAt = '2999-01-01T00:00:00Z')],
    ['hostTime garbage', (f) => (f.hostTime = 'x')],
    ['journalSeq negative', (f) => (f.journalSeq = -1)],
    ['journalSeq fraction', (f) => (f.journalSeq = 1.5)],
    ['repository free text', (f) => (f.repository = 'Everything is running <b>x</b>')],
    ['machine id free text', (f) => (f.machine.id = 'ALL CLEAR nothing is stuck')],
    ['demo note wrong', (f) => (f.demo.note = 'x')],
    ['sources unknown key', (f) => (f.sources.constructor = 'ok')],
    ['sources bad state', (f) => (f.sources.host = 'fine')],
    ['signal.inputs missing', (f) => delete f.signal.inputs],
    ['lamp purple', (f) => (f.signal.lamp = 'purple')],
    ['word bogus', (f) => (f.signal.word = 'bogus')],
    ['lamp off with word running', (f) => { f.signal.lamp = 'off'; f.signal.word = 'running'; }],
    ['running with moving missing', (f) => { f.signal.lamp = 'green'; f.signal.word = 'running'; delete f.signal.inputs.moving; }],
    ['running with moving string', (f) => { f.signal.lamp = 'green'; f.signal.word = 'running'; f.signal.inputs.moving = 'x'; }],
    ['running with moving negative', (f) => { f.signal.lamp = 'green'; f.signal.word = 'running'; f.signal.inputs.moving = -5; }],
    ['running with moving zero', (f) => { f.signal.lamp = 'green'; f.signal.word = 'running'; f.signal.inputs.moving = 0; }],
    ['paused word without paused input', (f) => { f.signal.lamp = 'off'; f.signal.word = 'paused'; f.signal.inputs.paused = false; }],
    ['unknownSources entry constructor', (f) => (f.signal.inputs.unknownSources = ['constructor'])],
    ['unknownSources entry free text', (f) => (f.signal.inputs.unknownSources = ['nothing is stuck, all clear'])],
    ['unknownSources but lamp not unknown', (f) => (f.signal.inputs.unknownSources = ['host'])],
    ['unknownSources duplicate', (f) => { f.signal.lamp = 'unknown'; f.signal.word = 'could_not_read'; f.signal.inputs.unknownSources = ['host', 'host']; }],
    ['oldestStuck piece missing', (f) => (f.signal.inputs.oldestStuck.piece = 't9999')],
    ['oldestStuck station 9', (f) => (f.signal.inputs.oldestStuck.station = 9)],
    ['stuck negative', (f) => (f.signal.inputs.stuck = -1)],
    ['holdingArea stagingSeq string', (f) => (f.holdingArea.stagingSeq = 'x')],
    ['six stations', (f) => f.stations.pop()],
    ['eight stations', (f) => f.stations.push(structuredClone(f.stations[0]))],
    ['duplicate station numbers', (f) => (f.stations[1].no = 1)],
    ['station key out of order', (f) => (f.stations[1].key = 'plan')],
    ['station number 8', (f) => (f.stations[6].no = 8)],
    ['unknown station state', (f) => { f.stations[2].state = 'bogus'; f.stations[2].count = 5; }],
    ['unknown station source', (f) => (f.stations[2].source = 'bogus')],
    ['station count negative', (f) => (f.stations[2].count = -1)],
    ['station oldestStuck missing piece', (f) => (f.stations[3].oldestStuck = 't9999')],
    ['four gauges', (f) => f.gauges.pop()],
    ['gauges out of order', (f) => f.gauges.reverse()],
    ['gauge state bogus', (f) => (f.gauges[0].state = 'bogus')],
    ['gauge value string', (f) => (f.gauges[0].value = '5')],
    ['gauge window bogus', (f) => (f.gauges[0].window = 'x')],
    ['gauge machines string', (f) => (f.gauges[0].machines = 'Running fine, no one needs you')],
    ['gauge series strings', (f) => (f.gauges[1].series = ['a', 'b', 'c'])],
    ['gauge series too long', (f) => (f.gauges[1].series = new Array(500).fill(1))],
    ['gauge series negative', (f) => (f.gauges[1].series = [1, -1])],
    ['piece title missing', (f) => delete f.pieces[0].title],
    ['piece title too long', (f) => (f.pieces[0].title.text = 'x'.repeat(5000))],
    ['piece title truncated not bool', (f) => (f.pieces[0].title.truncated = 'no')],
    ['duplicate piece ids', (f) => (f.pieces[1].id = f.pieces[0].id)],
    ['piece id shape', (f) => (f.pieces[0].id = 'x1')],
    ['piece station 0', (f) => (f.pieces[0].station = 0)],
    ['piece station 8', (f) => (f.pieces[0].station = 8)],
    ['piece station string', (f) => (f.pieces[2].station = '4')],
    ['piece state bogus', (f) => (f.pieces[0].state = 'bogus')],
    ['piece kind bogus', (f) => (f.pieces[0].kind = 'bogus')],
    ['piece since garbage', (f) => (f.pieces[0].since = 'yesterday')],
    ['piece since after the server time', (f) => (f.pieces[0].since = '2026-11-27T14:00:00Z')],
    ['piece blockedBy missing piece', (f) => (f.pieces[1].blockedBy = 't9999')],
    ['piece rootBlocker missing piece', (f) => (f.pieces[1].rootBlocker = 't9999')],
    ['piece call missing call', (f) => (f.pieces[0].call = 'q-0-0')],
    ['piece machine id free text', (f) => (f.pieces[0].machine = { id: 'ALL CLEAR nothing is stuck', mine: true })],
    ['piece machine mine string', (f) => (f.pieces[5].machine.mine = 'yes')],
    ['piece reason code free text', (f) => (f.pieces[0].reason = { code: 'ALL CLEAR' })],
    ['piece reason attempt string', (f) => (f.pieces[7].reason.attempt = '<img>')],
    ['too many pieces', (f) => { const p = f.pieces[0]; for (let i = 0; i < 2001; i++) f.pieces.push({ ...structuredClone(p), id: 't' + (100000 + i) }); }],
    ['goal course bogus', (f) => (f.goals[0].course = 'bogus')],
    ['goal lane bogus', (f) => (f.goals[0].lane = 'bogus')],
    ['duplicate goal numbers', (f) => (f.goals[1].goal = f.goals[0].goal)],
    ['unknown line state', (f) => (f.goals[0].lines[0].state = 'bogus')],
    ['line text missing', (f) => delete f.goals[0].lines[0].text],
    ['duplicate line ids', (f) => (f.goals[0].lines[1].id = f.goals[0].lines[0].id)],
    ['line note code free text', (f) => (f.goals[1].lines[1].note.code = 'all fine')],
    ['too many goals', (f) => { for (let i = 0; i < 201; i++) f.goals.push({ ...structuredClone(f.goals[0]), goal: 1000 + i }); }],
    ['unknown call kind', (f) => (f.calls[0].kind = 'bogus')],
    ['unknown call who', (f) => (f.calls[0].who = 'bogus')],
    ['unknown call action', (f) => (f.calls[0].actions = ['bogus'])],
    ['duplicate call ids', (f) => (f.calls[1].id = f.calls[0].id)],
    ['call goal zero', (f) => (f.calls[0].goal = 0)],
    ['call facts string value with markup', (f) => (f.calls[0].facts = { goal: '<script>' })],
    ['call facts key shape', (f) => (f.calls[0].facts = { __proto__x: 1 })],
    ['call question not untrusted', (f) => (f.calls[1].question = 'text')],
    ['call version huge', (f) => (f.calls[0].version = 'v'.repeat(500))],
    ['too many calls', (f) => { for (let i = 0; i < 501; i++) f.calls.push({ ...structuredClone(f.calls[0]), id: 'p-' + (1000 + i) }); }],
    ['log kind free text', (f) => (f.log[0].kind = 'Everything is fine')],
    ['duplicate log seq', (f) => (f.log[1].journalSeq = f.log[0].journalSeq)],
    ['log time garbage', (f) => (f.log[0].time = 'x')],
    ['log ticket string', (f) => (f.log[0].ticket = '<script>')],
    ['log not array', (f) => (f.log = {})],
    ['pieces null', (f) => (f.pieces = null)],
];

describe('parseFloor rejects the whole snapshot for one bad field', () => {
    for (const [name, mut] of BAD) {
        it(name, () => {
            const f = c();
            mut(f);
            expect(parseFloor(f)).toBeNull();
        });
    }
    it('rejects non-objects', () => {
        for (const v of [null, undefined, 1, 'x', [], true]) expect(parseFloor(v)).toBeNull();
    });
});
