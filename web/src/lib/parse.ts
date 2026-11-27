// The server's JSON is untrusted too. parseFloor accepts a snapshot only when every field has the right type and
// a known value. One failure rejects the whole snapshot: the page then says "Could not read" and never shows a
// mix of two snapshots. Where a value is a known string from a fixed set, the set is below.
import type { Floor } from './floor';

const LAMPS = ['red', 'amber', 'green', 'off', 'unknown'] as const;
const WORD_OF: Record<string, readonly string[]> = {
    unknown: ['could_not_read'], red: ['stuck'], amber: ['needs_a_person'], green: ['running'], off: ['paused', 'idle'],
};
const MODES = ['demo', 'live', 'unknown'];
const SOURCES = ['host', 'store', 'staging', 'gate', 'acceptance', 'trajectory', 'team', 'config'];
const SOURCE_STATES = ['ok', 'not_built', 'read_failed'];
const STATION_KEYS = ['plan', 'write_checks', 'build', 'check', 'second_opinion', 'holding_area', 'accepted'];
const WHO = ['person', 'agent', 'factory'];
const STATION_SOURCES = ['real', 'partly_real', 'scripted', 'not_built'];
const BAY_STATES = ['stuck', 'waiting_person', 'working', 'waiting_machine', 'ready', 'empty', 'unknown', 'not_built'];
const GAUGE_KEYS = ['in_work', 'accepted_today', 'right_first_time', 'goal_to_holding', 'attention'];
const GAUGE_STATES = ['ok', 'not_enough_yet', 'not_built', 'read_failed'];
const WINDOWS = ['now', 'today', '7d', '30d'];
const PIECE_KINDS = ['plan', 'checks', 'ticket'];
const PIECE_STATES = ['working', 'waiting_machine', 'waiting_person', 'stuck', 'ready'];
const LANES = ['quick', 'standard'];
const COURSES = ['on_course', 'drifting', 'off_course', 'not_started'];
const LINE_STATES = ['working', 'not_working', 'could_not_confirm'];
const CALL_KINDS = ['question', 'consequence', 'plan', 'accept', 'statement'];
const CALL_WHO = ['anyone_trusted', 'this_machine', 'a_developer'];
const ACTIONS = ['answer', 'yes', 'no', 'approve', 'accept', 'cancel'];

const MAX_PIECES = 2000, MAX_CALLS = 500, MAX_GOALS = 200, MAX_LOG = 200, MAX_LINES = 100, MAX_SERIES = 31;
const MAX_TITLE = 600, MAX_QUESTION = 20000, MAX_INT = 1_000_000_000;
const MIN_TIME = Date.UTC(2020, 0, 1), MAX_TIME = Date.UTC(2100, 0, 1);
const FUTURE_SLACK = 60_000;

const PIECE_ID = /^[tpk][0-9]{1,9}$/;
const CALL_ID = /^[A-Za-z0-9_-]{1,80}$/;
const RUN_ID = /^[A-Za-z0-9_]{1,64}$/;
const CODE = /^[a-z_]{1,40}$/;
const MACHINE = /^[A-Za-z0-9._-]{1,64}$/;
const REPO = /^[A-Za-z0-9._-]{1,100}\/[A-Za-z0-9._-]{1,100}$/;
const STORY = /^[a-z0-9_]{1,40}$/;
const COMMIT = /^[0-9a-f]{7,64}$/;
const FACT_KEY = /^[a-z][A-Za-z]{0,19}$/;
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;

class Bad extends Error {}
const fail = (): never => {
    throw new Bad();
};
const ok = (c: boolean): void => {
    if (!c) fail();
};

type Obj = Record<string, unknown>;
const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v);
const obj = (v: unknown): Obj => (isObj(v) ? v : (fail() as never));
const has = (o: Obj, k: string): boolean => Object.hasOwn(o, k);
const get = (o: Obj, k: string): unknown => (has(o, k) ? o[k] : undefined);
const arr = (v: unknown, max: number): unknown[] => {
    ok(Array.isArray(v) && v.length <= max);
    return v as unknown[];
};
const oneOf = (v: unknown, set: readonly string[]): string => {
    ok(typeof v === 'string' && set.includes(v));
    return v as string;
};
const int = (v: unknown, max = MAX_INT): number => {
    ok(typeof v === 'number' && Number.isSafeInteger(v) && v >= 0 && v <= max);
    return v as number;
};
const bool = (v: unknown): boolean => {
    ok(typeof v === 'boolean');
    return v as boolean;
};
const str = (v: unknown, re: RegExp): string => {
    ok(typeof v === 'string' && re.test(v));
    return v as string;
};
const nullable = <T,>(v: unknown, f: (x: unknown) => T): T | null => (v === null ? null : f(v));
const optional = <T,>(o: Obj, k: string, f: (x: unknown) => T): void => {
    if (has(o, k)) f(o[k]);
};

function time(v: unknown): number {
    ok(typeof v === 'string' && v.length <= 40 && RFC3339.test(v));
    const t = Date.parse(v as string);
    ok(!Number.isNaN(t) && t >= MIN_TIME && t <= MAX_TIME);
    return t;
}

function untrusted(v: unknown, max: number): void {
    const o = obj(v);
    ok(typeof o.text === 'string' && (o.text as string).length <= max);
    bool(o.truncated);
}

function reason(v: unknown): void {
    const o = obj(v);
    str(o.code, CODE);
    for (const k of ['count', 'attempt', 'maxAttempts']) optional(o, k, (x) => int(x, 1000));
    optional(o, 'criteria', (x) => arr(x, 100).forEach((n) => int(n, 1000)));
}

function facts(v: unknown): void {
    const o = obj(v);
    const keys = Object.keys(o);
    ok(keys.length <= 20);
    for (const k of keys) {
        ok(FACT_KEY.test(k));
        const x = o[k];
        if (typeof x === 'number') int(x);
        else str(x, MACHINE);
    }
}

export function parseFloor(input: unknown): Floor | null {
    try {
        validate(input);
        return input as Floor;
    } catch {
        return null;
    }
}

function validate(input: unknown): void {
    const f = obj(input);
    ok(f.schemaVersion === 1);
    int(f.journalSeq, Number.MAX_SAFE_INTEGER);
    const gen = time(f.generatedAt);
    nullable(f.hostTime, time);
    oneOf(f.mode, MODES);
    if (f.demo !== null) {
        const d = obj(f.demo);
        str(d.story, STORY);
        ok(d.note === 'made_up_project_fake_host');
    }
    ok(typeof f.repository === 'string' && (f.repository === '' || REPO.test(f.repository)));
    const mid = obj(f.machine).id;
    ok(typeof mid === 'string' && (mid === '' || MACHINE.test(mid)));
    const past = (v: unknown) => {
        const t = time(v);
        ok(t <= gen + FUTURE_SLACK);
    };

    // sources
    const src = obj(f.sources);
    for (const k of Object.keys(src)) {
        oneOf(k, SOURCES);
        oneOf(src[k], SOURCE_STATES);
    }

    // pieces first, so other parts can point at them
    const pieces = arr(f.pieces, MAX_PIECES).map(obj);
    const pieceIds = new Set<string>();
    for (const p of pieces) {
        const id = str(p.id, PIECE_ID);
        ok(!pieceIds.has(id));
        pieceIds.add(id);
    }
    const calls = arr(f.calls, MAX_CALLS).map(obj);
    const callIds = new Set<string>();
    for (const c of calls) {
        const id = str(c.id, CALL_ID);
        ok(!callIds.has(id));
        callIds.add(id);
    }
    const goals = arr(f.goals, MAX_GOALS).map(obj);
    const goalNos = new Set<number>();
    for (const g of goals) {
        const n = int(g.goal);
        ok(!goalNos.has(n));
        goalNos.add(n);
    }

    // signal
    const sig = obj(f.signal);
    const lamp = oneOf(sig.lamp, LAMPS);
    const word = oneOf(sig.word, WORD_OF[lamp] ?? []);
    const inp = obj(sig.inputs);
    const moving = int(inp.moving);
    nullable(inp.movingElsewhere, (x) => int(x));
    const stuck = int(inp.stuck);
    int(inp.waitingForPerson);
    nullable(inp.inHoldingArea, (x) => int(x));
    if (inp.oldestStuck !== null) {
        const o = obj(inp.oldestStuck);
        ok(pieceIds.has(str(o.piece, PIECE_ID)));
        ok(Number.isSafeInteger(o.station) && (o.station as number) >= 1 && (o.station as number) <= 7);
        past(o.since);
    }
    const paused = bool(inp.paused);
    const checksFailing = bool(inp.projectChecksFailing);
    const unknownSources = arr(inp.unknownSources, SOURCES.length).map((s) => oneOf(s, SOURCES));
    ok(new Set(unknownSources).size === unknownSources.length);
    // The word has to agree with the numbers behind it.
    ok((lamp === 'unknown') === (unknownSources.length > 0));
    if (lamp === 'green') ok(moving > 0 && !paused);
    if (lamp === 'red') ok(stuck > 0 || checksFailing);
    if (word === 'paused') ok(paused);

    nullable(f.holdingArea, (x) => {
        const h = obj(x);
        int(h.stagingSeq, Number.MAX_SAFE_INTEGER);
        str(h.commit, COMMIT);
        bool(h.published);
        bool(h.frozen);
        nullable(h.pendingAccept, (y) => {
            const a = obj(y);
            nullable(a.since, time);
            oneOf(a.mode, ['merge', 'developer']);
            int(a.pullRequest);
        });
    });

    // gauges: five, in a fixed order
    const gauges = arr(f.gauges, 5).map(obj);
    ok(gauges.length === 5);
    gauges.forEach((g, i) => {
        ok(g.key === GAUGE_KEYS[i]);
        oneOf(g.state, GAUGE_STATES);
        nullable(g.value, (x) => {
            ok(typeof x === 'number' && Number.isSafeInteger(x));
        });
        nullable(g.samples, (x) => int(x));
        oneOf(g.window, WINDOWS);
        optional(g, 'series', (x) => arr(x, MAX_SERIES).forEach((n) => int(n)));
        optional(g, 'machines', (x) => int(x, 1000));
    });

    // stations: seven, in a fixed order
    const stations = arr(f.stations, 7).map(obj);
    ok(stations.length === 7);
    stations.forEach((s, i) => {
        ok(s.no === i + 1);
        ok(s.key === STATION_KEYS[i]);
        oneOf(s.who, WHO);
        oneOf(s.source, STATION_SOURCES);
        oneOf(s.state, BAY_STATES);
        nullable(s.count, (x) => int(x, MAX_PIECES));
        nullable(s.oldestStuck, (x) => ok(pieceIds.has(str(x, PIECE_ID))));
    });

    // pieces
    for (const p of pieces) {
        oneOf(p.kind, PIECE_KINDS);
        int(p.goal);
        nullable(p.ticket, (x) => int(x));
        untrusted(p.title, MAX_TITLE);
        const st = p.station;
        ok(typeof st === 'number' && Number.isSafeInteger(st) && st >= 1 && st <= 7);
        oneOf(p.state, PIECE_STATES);
        nullable(p.reason, reason);
        nullable(p.since, past);
        for (const k of ['blockedBy', 'rootBlocker']) nullable(p[k], (x) => ok(pieceIds.has(str(x, PIECE_ID))));
        nullable(p.machine, (x) => {
            const m = obj(x);
            str(m.id, MACHINE);
            bool(m.mine);
        });
        nullable(p.claim, (x) => {
            const c = obj(x);
            time(c.since);
            time(c.expires);
        });
        nullable(p.run, (x) => str(x, RUN_ID));
        nullable(p.call, (x) => ok(callIds.has(str(x, CALL_ID))));
    }

    // goals
    for (const g of goals) {
        untrusted(g.title, MAX_TITLE);
        nullable(g.lane, (x) => oneOf(x, LANES));
        nullable(g.course, (x) => oneOf(x, COURSES));
        nullable(g.lines, (x) => {
            const ids = new Set<number>();
            for (const l of arr(x, MAX_LINES).map(obj)) {
                const id = int(l.id);
                ok(!ids.has(id));
                ids.add(id);
                untrusted(l.text, MAX_TITLE);
                oneOf(l.state, LINE_STATES);
                nullable(l.note, (y) => {
                    const n = obj(y);
                    str(n.code, CODE);
                    optional(n, 'ticket', (z) => int(z));
                });
            }
        });
        nullable(g.linesAsOf, (x) => int(obj(x).stagingSeq, Number.MAX_SAFE_INTEGER));
    }

    // calls
    for (const c of calls) {
        oneOf(c.kind, CALL_KINDS);
        oneOf(c.who, CALL_WHO);
        nullable(c.since, past);
        nullable(c.goal, (x) => ok(goalNos.has(int(x))));
        nullable(c.ticket, (x) => int(x));
        nullable(c.question, (x) => untrusted(x, MAX_QUESTION));
        facts(c.facts);
        const acts = arr(c.actions, 6).map((a) => oneOf(a, ACTIONS));
        ok(new Set(acts).size === acts.length);
        ok(typeof c.version === 'string' && c.version.length <= 128);
    }

    // log
    const seqs = new Set<number>();
    for (const e of arr(f.log, MAX_LOG).map(obj)) {
        const n = int(e.journalSeq, Number.MAX_SAFE_INTEGER);
        ok(!seqs.has(n));
        seqs.add(n);
        nullable(e.time, past);
        str(e.kind, CODE);
        optional(e, 'goal', (x) => int(x));
        optional(e, 'ticket', (x) => int(x));
        optional(e, 'facts', facts);
    }
}
