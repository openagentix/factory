// The JSON of GET /api/v1/floor, as record 0004 and internal/floor define it.
// Where Go sends null for something unknown, the type says so. Nothing here
// is a default: an unknown value stays null.

export type Lamp = 'red' | 'amber' | 'green' | 'off' | 'unknown';
export type LampWord = 'could_not_read' | 'stuck' | 'needs_a_person' | 'running' | 'paused' | 'idle';
export type Mode = 'demo' | 'live' | 'unknown';
export type SourceKey = 'host' | 'store' | 'staging' | 'gate' | 'acceptance' | 'trajectory' | 'team' | 'config';
export type SourceState = 'ok' | 'not_built' | 'read_failed';
export type StationNo = 1 | 2 | 3 | 4 | 5 | 6 | 7;
export type StationKey =
    | 'plan'
    | 'write_checks'
    | 'build'
    | 'check'
    | 'second_opinion'
    | 'holding_area'
    | 'accepted';
export type Who = 'person' | 'agent' | 'factory';
export type StationSource = 'real' | 'partly_real' | 'scripted' | 'not_built';
export type BayState =
    | 'stuck'
    | 'waiting_person'
    | 'working'
    | 'waiting_machine'
    | 'ready'
    | 'empty'
    | 'unknown'
    | 'not_built';
export type GaugeKey = 'in_work' | 'accepted_today' | 'right_first_time' | 'goal_to_holding' | 'attention';
export type GaugeState = 'ok' | 'not_enough_yet' | 'not_built' | 'read_failed';
export type PieceKind = 'plan' | 'checks' | 'ticket';
export type PieceState = 'working' | 'waiting_machine' | 'waiting_person' | 'stuck' | 'ready';
export type Lane = 'quick' | 'standard';
export type Course = 'on_course' | 'drifting' | 'off_course' | 'not_started';
export type LineState = 'working' | 'not_working' | 'could_not_confirm';
export type CallKind = 'question' | 'consequence' | 'plan' | 'accept' | 'statement';
export type CallWho = 'anyone_trusted' | 'this_machine' | 'a_developer';
export type ActionName = 'answer' | 'yes' | 'no' | 'approve' | 'accept' | 'cancel';
// Reason codes and journal kinds are strings the server may extend. The page treats an unknown one as unreadable.
export type ReasonCode = string;
export type JournalKind = string;
export type Fact = number | string;

export type Untrusted = { text: string; truncated: boolean };

export type Reason = { code: ReasonCode; count?: number; attempt?: number; maxAttempts?: number; criteria?: number[] };

export type Signal = {
    lamp: Lamp;
    word: LampWord;
    inputs: {
        moving: number;
        movingElsewhere: number | null;
        stuck: number;
        waitingForPerson: number;
        inHoldingArea: number | null;
        oldestStuck: null | { piece: string; station: StationNo; since: string };
        paused: boolean;
        projectChecksFailing: boolean;
        unknownSources: SourceKey[];
    };
};

export type HoldingArea = {
    stagingSeq: number;
    commit: string;
    published: boolean;
    frozen: boolean;
    pendingAccept: null | { since: string | null; mode: 'merge' | 'developer'; pullRequest: number };
};

export type Gauge = {
    key: GaugeKey;
    state: GaugeState;
    value: number | null;
    samples: number | null;
    window: 'now' | 'today' | '7d' | '30d';
    series?: number[];
    machines?: number;
};

export type Station = {
    no: StationNo;
    key: StationKey;
    who: Who;
    source: StationSource;
    state: BayState;
    count: number | null;
    oldestStuck: string | null;
};

export type Piece = {
    id: string;
    kind: PieceKind;
    goal: number;
    ticket: number | null;
    title: Untrusted;
    station: StationNo;
    state: PieceState;
    reason: Reason | null;
    since: string | null;
    blockedBy: string | null;
    rootBlocker: string | null;
    machine: null | { id: string; mine: boolean };
    claim: null | { since: string; expires: string };
    run: string | null;
    call: string | null;
};

export type GoalLine = {
    id: number;
    text: Untrusted;
    state: LineState;
    note: null | { code: ReasonCode; ticket?: number };
};

export type Goal = {
    goal: number;
    title: Untrusted;
    lane: Lane | null;
    course: Course | null;
    lines: GoalLine[] | null;
    linesAsOf: null | { stagingSeq: number };
};

export type Call = {
    id: string;
    kind: CallKind;
    who: CallWho;
    since: string | null;
    goal: number | null;
    ticket: number | null;
    question: Untrusted | null;
    facts: Record<string, Fact>;
    actions: ActionName[];
    version: string;
};

export type JournalEntry = {
    journalSeq: number;
    time: string | null;
    kind: JournalKind;
    goal?: number;
    ticket?: number;
    facts?: Record<string, Fact>;
};

export type Floor = {
    schemaVersion: number;
    journalSeq: number;
    generatedAt: string;
    hostTime: string | null;
    mode: Mode;
    demo: null | { story: string; note: string };
    repository: string;
    machine: { id: string };
    sources: Partial<Record<SourceKey, SourceState>>;
    signal: Signal;
    holdingArea: HoldingArea | null;
    gauges: Gauge[];
    stations: Station[];
    pieces: Piece[];
    goals: Goal[];
    calls: Call[];
    log: JournalEntry[];
};

/** The one thing a snapshot must have for the page to draw it: seven stations and five gauges. */
export function isFloor(v: unknown): v is Floor {
    if (typeof v !== 'object' || v === null) return false;
    const f = v as Partial<Floor>;
    return (
        f.schemaVersion === 1 &&
        typeof f.generatedAt === 'string' &&
        Array.isArray(f.stations) &&
        f.stations.length === 7 &&
        Array.isArray(f.gauges) &&
        Array.isArray(f.pieces) &&
        Array.isArray(f.goals) &&
        Array.isArray(f.calls) &&
        Array.isArray(f.log) &&
        typeof f.signal === 'object' &&
        f.signal !== null &&
        typeof f.mode === 'string'
    );
}
