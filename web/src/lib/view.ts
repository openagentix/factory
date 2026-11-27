// What the screens show, worked out from a snapshot. Pure functions, so they can be tested without a page.
import type { Call, Floor, Gauge, Goal, Piece, SourceKey, Station, StationNo } from './floor';
import { COULD_NOT_READ, NOT_BUILT, NOT_ENOUGH, ageText, fill, sentence } from './text';

export const STATION_NAMES = ['Plan', 'Write checks', 'Build', 'Check', 'Second opinion', 'Holding area', 'Accepted'] as const;
export const GATES = ['You approve the plan', '', '', '', '', '', 'You accept'];

export function stationName(n: number): string {
    return STATION_NAMES[n - 1] ?? COULD_NOT_READ;
}

export function two(n: number): string {
    return (n < 10 ? '0' : '') + n;
}

const SOURCE_LABEL: Record<SourceKey, string> = {
    host: 'the project host',
    store: "the factory's records",
    staging: 'the holding area',
    gate: 'the checks',
    acceptance: 'the lines',
    trajectory: 'the goal course',
    team: 'the team list',
    config: 'the settings',
};

// Fixed templates for the parts of the signal sentence that the phrase table does not hold.
// They go through the same fill() function, so they take numbers only.
const UI = {
    stuck: '{count} stuck for {age} at {station}.',
    waits: '{count} things wait for a person.',
    waits1: '1 thing waits for a person.',
    holding: '{count} in the holding area, ready for you to accept.',
};

export type Part = { text: string; cls: '' | 'c-bad' | 'c-wait'; bold: boolean };
export type SignalView = { lamp: string; word: string; parts: Part[]; tone: 'bad' | 'wait' | 'ok' | 'idle' | 'unk' };

/** The words of the status header. Unknown and not-built values are left out, never shown as a number. */
/** A green lamp needs a safe positive count of moving pieces. Anything else is not trusted as Running. */
function beltOk(f: Floor): boolean {
    return f.signal.lamp !== 'green' || (Number.isSafeInteger(f.signal.inputs.moving) && f.signal.inputs.moving > 0);
}

export function signalView(f: Floor, nowMs: number, stale: boolean): SignalView {
    if (stale || f.signal.lamp === 'unknown' || !beltOk(f)) {
        const bad = f.signal.inputs.unknownSources.map(sourceLabel);
        const parts: Part[] = stale ? [] : bad.length ? [{ text: `The factory could not read ${bad.join(', ')}.`, cls: '', bold: false }] : [];
        return { lamp: 'unknown', word: 'could_not_read', parts, tone: 'unk' };
    }
    const i = f.signal.inputs;
    const parts: Part[] = [];
    const mv = sentence('signal.moving', { moving: i.moving });
    if (mv) parts.push({ text: mv, cls: '', bold: true });
    if (i.movingElsewhere !== null && i.movingElsewhere > 0) {
        const s = sentence('signal.elsewhere', { elsewhere: i.movingElsewhere });
        if (s) parts.push({ text: s, cls: '', bold: false });
    }
    if (i.stuck > 0 && i.oldestStuck) {
        const since = Date.parse(i.oldestStuck.since);
        const s = Number.isNaN(since) ? null : fillUi(UI.stuck, { count: i.stuck, age: nowMs - since, station: i.oldestStuck.station });
        if (s) parts.push({ text: s, cls: 'c-bad', bold: true });
    }
    if (i.waitingForPerson > 0) {
        const s = i.waitingForPerson === 1 ? UI.waits1 : fillUi(UI.waits, { count: i.waitingForPerson });
        parts.push({ text: s, cls: 'c-wait', bold: true });
    }
    if (i.inHoldingArea !== null && i.inHoldingArea > 0) parts.push({ text: fillUi(UI.holding, { count: i.inHoldingArea }), cls: '', bold: false });
    const tone = f.signal.lamp === 'red' ? 'bad' : f.signal.lamp === 'amber' ? 'wait' : f.signal.lamp === 'green' ? 'ok' : 'idle';
    return { lamp: f.signal.lamp, word: f.signal.word, parts, tone };
}

/** The stuck template shows minutes and the station's name, which are not whole-number slots. They are formatted here, then checked. */
function fillUi(t: string, v: { count: number; age?: number; station?: StationNo }): string {
    if (v.age !== undefined && v.station !== undefined) {
        return fill(t.replace('{age}', ageOrUnreadable(v.age)).replace('{station}', stationName(v.station)), { count: v.count });
    }
    return fill(t, { count: v.count });
}

export type GaugeView = {
    key: Gauge['key'];
    label: string;
    kind: 'value' | 'honest';
    value: string | null;
    unit: string;
    detail: string;
    honest: string | null;
    series: number[] | null;
};

const LABELS: Record<Gauge['key'], string> = {
    in_work: 'In work',
    accepted_today: 'Accepted today',
    right_first_time: 'Right first time',
    goal_to_holding: 'Goal to holding area',
    attention: 'Your attention',
};

export function gaugeView(g: Gauge): GaugeView {
    const base = { key: g.key, label: pick(LABELS, g.key) ?? COULD_NOT_READ, series: null as number[] | null };
    const honest = (text: string): GaugeView => ({ ...base, kind: 'honest', value: null, unit: '', detail: '', honest: text });
    // A value is shown only when the state is ok and the value is a whole number. Anything else is an honest state.
    if (g.state === 'not_built') return honest(NOT_BUILT);
    if (g.state === 'not_enough_yet') return honest(NOT_ENOUGH);
    if (g.state === 'read_failed' || g.value === null || !Number.isSafeInteger(g.value)) return honest(COULD_NOT_READ);
    const v = g.value;
    const max = g.key === 'right_first_time' ? 100 : 1_000_000_000;
    if (v < 0 || v > max) return honest(COULD_NOT_READ);
    switch (g.key) {
        case 'in_work':
            return { ...base, kind: 'value', value: String(v), unit: v === 1 ? 'piece on the line' : 'pieces on the line', honest: null,
                detail: g.machines === undefined || !Number.isSafeInteger(g.machines) || g.machines < 0 ? '' : `${g.machines} ${g.machines === 1 ? 'machine' : 'machines'}, one job each at a time` };
        case 'accepted_today':
            return { ...base, kind: 'value', value: String(v), unit: v === 1 ? 'change' : 'changes', detail: '', honest: null, series: g.series ?? null };
        case 'right_first_time':
            return { ...base, kind: 'value', value: v + '%', unit: 'last 30 days', honest: null, detail: 'Changes that passed every check on the first try' };
        case 'goal_to_holding':
            return { ...base, kind: 'value', value: String(Math.round(v / 60)), unit: 'min, typical', honest: null, detail: 'From an approved plan to a checked change' };
        case 'attention':
            return { ...base, kind: 'value', value: (v / 100).toFixed(1), unit: 'touches per goal', honest: null, detail: 'Person actions per goal, last 7 days' };
    }
    return honest(COULD_NOT_READ);
}

export function sparkPoints(series: number[], w = 120, h = 22): { line: string; area: string; last: [number, number] } | null {
    if (series.length < 2) return null;
    const max = Math.max(1, ...series);
    const pts = series.map((n, i): [number, number] => [Math.round((i / (series.length - 1)) * w), Math.round(h - 4 - (n / max) * (h - 8))]);
    const line = pts.map(([x, y], i) => (i === 0 ? 'M' : 'L') + x + ' ' + y).join(' ');
    return { line, area: `${line} L${w} ${h} L0 ${h}z`, last: pts[pts.length - 1] as [number, number] };
}

export function piecesAt(f: Floor, n: StationNo): Piece[] {
    return f.pieces.filter((p) => p.station === n);
}

export function sinceMs(iso: string | null): number | null {
    if (iso === null) return null;
    const t = Date.parse(iso);
    return Number.isNaN(t) ? null : t;
}

/** The age of a piece in its state, from the server's time plus the time since it arrived. */
export function pieceAge(p: Piece, nowMs: number): string | null {
    const t = sinceMs(p.since);
    return t === null ? null : ageOrUnreadable(nowMs - t);
}

export function oldestAt(f: Floor, n: StationNo, nowMs: number): string | null {
    let best: number | null = null;
    for (const p of piecesAt(f, n)) {
        const t = sinceMs(p.since);
        if (t !== null && (best === null || t < best)) best = t;
    }
    return best === null ? null : ageOrUnreadable(nowMs - best);
}

export function pieceById(f: Floor, id: string | null): Piece | null {
    return id === null ? null : (f.pieces.find((p) => p.id === id) ?? null);
}

export function goalById(f: Floor, n: number | null): Goal | null {
    return n === null ? null : (f.goals.find((g) => g.goal === n) ?? null);
}

export function bayClass(s: Station): string {
    switch (typeof s.state === 'string' ? s.state : '') {
        case 'stuck': return 's-stuck';
        case 'waiting_person': return 's-person';
        case 'working': return 's-working';
        case 'unknown': return 's-unknown';
        case 'not_built': return 's-notbuilt';
        default: return '';
    }
}

/** Holding-area changes grouped by goal, for the one Accept call. */
export function acceptGoals(f: Floor): { goal: number; title: string | null; count: number }[] {
    const m = new Map<number, number>();
    for (const p of f.pieces) if (p.station === 6) m.set(p.goal, (m.get(p.goal) ?? 0) + 1);
    return [...m.entries()].sort((a, b) => a[0] - b[0]).map(([goal, count]) => ({ goal, title: goalById(f, goal)?.title.text ?? null, count }));
}

export function callTitle(f: Floor, c: Call): string | null {
    const t = c.ticket === null ? null : f.pieces.find((p) => p.ticket === c.ticket)?.title.text;
    if (t) return t;
    const g = goalById(f, c.goal);
    return g ? g.title.text : null;
}

export function lineCounts(g: Goal): { ok: number; total: number } | null {
    if (g.lines === null) return null;
    return { ok: g.lines.filter((l) => l.state === 'working').length, total: g.lines.length };
}

// ---- how a piece looks: always an icon and a word ----

export type Look = { cls: string; icon: string; word: string; chip: string; dot: string; tag: string };

export function pieceLook(p: Piece, unreadable: boolean): Look {
    if (unreadable) return { cls: 'c-idle', icon: 'unk', word: COULD_NOT_READ, chip: 'idle', dot: 'unknown', tag: 'unknown' };
    switch (p.state) {
        case 'working': return { cls: 'c-ok', icon: 'work', word: 'Working', chip: 'ok', dot: 'working', tag: 'working' };
        case 'waiting_machine': return { cls: 'c-idle', icon: 'queue', word: 'Waiting for a machine', chip: 'idle', dot: 'queued', tag: '' };
        case 'waiting_person': return { cls: 'c-wait', icon: 'wait', word: 'Waiting for a person', chip: 'wait', dot: 'person', tag: 'person' };
        case 'stuck': return { cls: 'c-bad', icon: 'stuck', word: 'Stuck', chip: 'bad', dot: 'stuck', tag: 'stuck' };
        case 'ready': return { cls: 'c-flow', icon: 'done', word: p.station === 7 ? 'Accepted' : 'Ready', chip: 'flow', dot: 'done', tag: 'done' };
    }
    return { cls: 'c-idle', icon: 'unk', word: COULD_NOT_READ, chip: 'idle', dot: 'unknown', tag: 'unknown' };
}

export function pieceName(p: Piece): string {
    return p.kind === 'ticket' && p.ticket !== null ? `#${p.ticket}` : p.kind === 'plan' ? `Plan, goal ${p.goal}` : `Checks, goal ${p.goal}`;
}

export const LINE_LOOK = {
    working: { chip: 'ok', icon: 'ok', word: 'Working' },
    not_working: { chip: 'wait', icon: 'no', word: 'Not working yet' },
    could_not_confirm: { chip: 'idle', icon: 'unk', word: 'Could not confirm' },
} as const;

export const COURSE_LOOK = {
    on_course: { chip: 'ok', icon: 'ok', word: 'On course' },
    drifting: { chip: 'wait', icon: 'stuck', word: 'Drifting' },
    off_course: { chip: 'bad', icon: 'stuck', word: 'Off course' },
    not_started: { chip: 'idle', icon: 'queue', word: 'Not started' },
} as const;

export const CALL_KIND_LOOK = {
    question: { chip: 'wait', icon: 'wait', word: 'Question' },
    consequence: { chip: 'wait', icon: 'stuck', word: 'Yes or no' },
    plan: { chip: 'wait', icon: 'wait', word: 'Plan to approve' },
    accept: { chip: 'flow', icon: 'done', word: 'Ready to accept' },
    statement: { chip: 'bad', icon: 'stuck', word: 'A developer is needed' },
} as const;

export const WHO_WORD = { anyone_trusted: 'Anyone trusted', this_machine: 'This machine', a_developer: 'A developer' } as const;

export const LOG_LOOK: Record<string, { cls: string; icon: string }> = {
    entered_holding_area: { cls: 'c-ok', icon: 'ok' },
    accepted: { cls: 'c-ok', icon: 'ok' },
    plan_approved: { cls: 'c-ok', icon: 'ok' },
    checks_written: { cls: 'c-ok', icon: 'ok' },
    answered: { cls: 'c-ok', icon: 'ok' },
    consequence_answered: { cls: 'c-ok', icon: 'ok' },
    check_failed: { cls: 'c-bad', icon: 'stuck' },
    taken_out: { cls: 'c-wait', icon: 'back' },
    undone: { cls: 'c-wait', icon: 'back' },
    question_asked: { cls: 'c-wait', icon: 'wait' },
    could_not_confirm: { cls: 'c-idle', icon: 'unk' },
};

/** A short spoken summary of where a goal's pieces are, for the mini line. */
export function miniSummary(f: Floor, goal: number): string {
    const parts: string[] = [];
    for (let n = 1; n <= 7; n++) {
        const here = f.pieces.filter((p) => p.goal === goal && p.station === n);
        if (here.length) parts.push(`${two(n)} ${stationName(n)}: ${here.length}`);
    }
    return parts.length ? 'Pieces of this goal by station. ' + parts.join('. ') : 'No pieces of this goal are on the line.';
}

// ---- defensive lookups: a value from the server never picks a property of the table itself ----

/** A table entry for a key that came from the server. Own properties only. */
export function pick<T>(table: Record<string, T>, key: unknown): T | undefined {
    return typeof key === 'string' && Object.hasOwn(table, key) ? table[key] : undefined;
}

export function sourceLabel(k: unknown): string {
    return pick(SOURCE_LABEL, k) ?? COULD_NOT_READ;
}

/** Anything but exactly demo or live is unknown. */
export function modeKind(mode: unknown): 'demo' | 'live' | 'unknown' {
    return mode === 'demo' || mode === 'live' ? mode : 'unknown';
}

/** An age is shown only for a usable server time. A negative or unusable age reads Could not read. */
export function ageOrUnreadable(ms: number): string {
    return typeof ms === 'number' && Number.isFinite(ms) && ms >= 0 ? ageText(ms) : COULD_NOT_READ;
}

/** The belt moves only for a safe positive count of moving pieces, a known lamp, a line that is not paused, and a good state. */
export function beltMoves(f: Floor, failed: boolean): boolean {
    const i = f.signal?.inputs;
    if (failed || !i) return false;
    const lamp = f.signal.lamp;
    if (lamp !== 'green' && lamp !== 'amber' && lamp !== 'red' && lamp !== 'off') return false;
    return typeof i.moving === 'number' && Number.isSafeInteger(i.moving) && i.moving > 0 && i.paused === false;
}

/** What a chip says when its value is not one the page knows. */
export const UNREAD = { chip: 'idle', icon: 'unk', word: COULD_NOT_READ } as const;
