// The state of the page: the key, the latest snapshot, the clock, the route and the open drawer.
import { startClient, type Client } from './client';
import { parseDetail, parseUndoPreview } from './detail';
import type { Call, Floor, PieceDetail, UndoPreview, UndoRef } from './floor';
import { readKeys, rememberKey, type KeyEnv } from './key';
import { lampWord } from './text';
import { modeKind, signalView } from './view';

export const ROUTES = ['floor', 'needs-you', 'goals', 'live', 'team', 'runs', 'autonomy', 'plant', 'record'] as const;
export type Route = (typeof ROUTES)[number];

export function parseRoute(hash: string): Route {
    // The fragment is exactly a route name. Anything else is the Floor.
    const name = hash.replace(/^#/, '');
    return (ROUTES as readonly string[]).includes(name) ? (name as Route) : 'floor';
}

export type Env = KeyEnv & {
    fetch: typeof fetch;
    localStorage: Pick<Storage, 'getItem' | 'setItem'>;
    perfNow: () => number;
    matchMedia?: (q: string) => { matches: boolean };
};

/**
 * One press of a button: sending; sent (done, waiting for a snapshot to show it); running (a 202: still being done); again (a later
 * snapshot still has the call, so the button is live and a press sends the same key); refused with the server's code; or failed with no answer.
 */
export type Act = { phase: 'sending' | 'sent' | 'running' | 'again' | 'refused' | 'failed'; code?: string };
export type ActionKind = 'answer' | 'yes' | 'no' | 'approve' | 'accept' | 'cancel';

/** The piece drawer's detail, read from GET /api/v1/pieces/{id}. Old detail stays until the new one is read. */
export type DetailView = { id: string; phase: 'loading' | 'ready' | 'failed'; detail: PieceDetail | null };

/**
 * One Undo, from the piece drawer. It always reads what leaves first (loading, then preview), and sends nothing until the
 * person presses "Undo now". A failed read keeps its code, so the screen says why.
 */
export type UndoFlow = { piece: string; ref: UndoRef; phase: 'loading' | 'preview' | 'failed'; preview: UndoPreview | null; code?: string };
export const undoKey = (r: UndoRef) => `u|${r.stagingSeq}|${r.commit}|${r.change}`;
const actKey = (c: Pick<Call, 'id' | 'version'>) => `${c.id}|${c.version}`;

/** A fresh random Idempotency-Key. It has nothing to do with the login key. */
function newIdemKey(): string {
    const b = new Uint8Array(24);
    globalThis.crypto.getRandomValues(b);
    return btoa(String.fromCharCode(...b)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export type Status = 'loading' | 'nokey' | 'denied' | 'ready';

export class AppState {
    status = $state<Status>('loading');
    floor = $state.raw<Floor | null>(null);
    /** The last read failed, or the page could not draw what it read. The snapshot, if any, is old. */
    failed = $state(false);
    /** A screen threw while drawing. Cleared by the next good snapshot. */
    crashed = $state(false);
    /** Changes after a crash, so the screen is drawn again from nothing. */
    crashEpoch = $state(0);
    route = $state<Route>('floor');
    openGoals = $state<Record<number, boolean>>({});
    drawerId = $state<string | null>(null);
    detail = $state.raw<DetailView | null>(null);
    undo = $state.raw<UndoFlow | null>(null);
    announce = $state('');
    /** Ids of pieces whose station changed between the last two snapshots. */
    fresh = $state.raw<ReadonlySet<string>>(new Set());
    reduceMotion = false;
    /** The state of each button press, by call id and version. Only a later snapshot clears a sent press. */
    acts = $state.raw<Readonly<Record<string, Act>>>({});
    /** The Idempotency-Key of a press, kept until the snapshot shows the result, so a repeat sends the same key. */
    private idem = new Map<string, { key: string; body: string }>();
    /** Counts the reads of a piece, so a slow old reply never replaces a newer one. */
    private detailN = 0;

    private tick = $state(0);
    private serverMs = 0;
    private recvPerf = 0;
    private lastSignal = '';
    private client: Client | null = null;
    private timer: ReturnType<typeof setInterval> | null = null;
    private opener: HTMLElement | null = null;

    constructor(readonly env: Env) {
        this.reduceMotion = !!env.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
    }

    /** The server's time now: the snapshot's time plus the time since it arrived. The browser's own clock is never used. */
    nowMs(): number {
        void this.tick;
        return this.serverMs + (this.env.perfNow() - this.recvPerf);
    }

    get demo(): boolean {
        return modeKind(this.floor?.mode) === 'demo';
    }

    start(): void {
        this.route = parseRoute(this.env.location.hash);
        const { candidate, stored } = readKeys(this.env);
        const key = candidate ?? stored;
        if (key === null) {
            this.status = 'nokey';
            return;
        }
        this.status = 'loading';
        this.begin(key, candidate !== null, candidate !== null ? stored : null);
        this.timer = setInterval(() => this.tick++, 15000);
    }

    /** A candidate key is kept only after the server accepts it. If it is refused, the stored key is used again. */
    private begin(key: string, isCandidate: boolean, fallback: string | null): void {
        this.client?.stop();
        this.client = startClient({
            key,
            fetch: this.env.fetch,
            onSnapshot: (f) => {
                if (isCandidate) {
                    isCandidate = false;
                    rememberKey(this.env, key);
                }
                this.take(f);
            },
            onFailure: (k) => {
                if (k === 'denied') {
                    if (isCandidate && fallback !== null) {
                        this.begin(fallback, false, null);
                        return;
                    }
                    this.status = 'denied';
                    return;
                }
                this.fail('Could not read. The page could not reach the factory.');
            },
        });
    }

    /**
     * Presses a button of a call. The reply changes nothing on the screen: the call stays where it is until a later
     * snapshot no longer has it at this version. A refusal or a failure keeps the call and says why.
     */
    async act(call: Call, action: ActionKind, text?: string): Promise<void> {
        const k = actKey(call);
        const cur = this.acts[k];
        if (cur && (cur.phase === 'sending' || cur.phase === 'sent' || cur.phase === 'running')) return;
        if (!(call.actions as string[]).includes(action) || this.client === null) return;
        let path = `/api/v1/calls/${encodeURIComponent(call.id)}/${action}`;
        const body: Record<string, string | number> = { version: call.version };
        if (action === 'answer') body.text = (text ?? '').replace(/\r\n?/g, '\n');
        if (action === 'accept' || action === 'cancel') {
            // The Accept call is for one state of the holding area. The page sends that state, and only when the call is for it.
            const h = this.floor?.holdingArea ?? null;
            if (h === null || call.id !== `a-${h.stagingSeq}`) {
                this.acts = { ...this.acts, [k]: { phase: 'refused', code: 'stale' } };
                return;
            }
            path = `/api/v1/holding-area/${action}`;
            body.stagingSeq = h.stagingSeq;
            body.commit = h.commit;
        }
        await this.send(k, path, body);
    }

    /** One press, from its key to its message. The key of the press stays the same for a repeat of the same body. */
    private async send(k: string, path: string, body: Record<string, unknown>): Promise<void> {
        if (this.client === null) return;
        const json = JSON.stringify(body);
        let held = this.idem.get(k);
        if (!held || held.body !== json) {
            held = { key: newIdemKey(), body: json };
            this.idem.set(k, held);
        }
        this.acts = { ...this.acts, [k]: { phase: 'sending' } };
        const r = await this.client.write(path, body, held.key);
        // A snapshot may have dropped this press while it was sent. Then nothing is left to say.
        if (!(k in this.acts)) return;
        const next: Act = r.ok ? { phase: r.status === 202 ? 'running' : 'sent' } : r.status === 0 ? { phase: 'failed' } : { phase: 'refused', code: r.code };
        this.acts = { ...this.acts, [k]: next };
    }

    /** Reads one piece for the drawer. A reply that is not whole is "Could not read". */
    async loadDetail(id: string): Promise<void> {
        if (this.client === null) return;
        const keep = this.detail !== null && this.detail.id === id ? this.detail.detail : null;
        this.detail = { id, phase: keep === null ? 'loading' : 'ready', detail: keep };
        const n = ++this.detailN;
        const r = await this.client.read(`/api/v1/pieces/${encodeURIComponent(id)}`);
        if (this.drawerId !== id || n !== this.detailN) return; // the drawer was closed or moved on, or a newer read is under way
        const d = r.ok ? parseDetail(r.body) : null;
        this.detail = d !== null && d.piece.id === id ? { id, phase: 'ready', detail: d } : { id, phase: 'failed', detail: null };
    }

    /** Starts an Undo: reads what would leave with the change. Nothing is sent. */
    async startUndo(piece: string, ref: UndoRef): Promise<void> {
        if (this.client === null) return;
        this.undo = { piece, ref, phase: 'loading', preview: null };
        const r = await this.client.read(`/api/v1/changes/${encodeURIComponent(ref.change)}/undo-preview`);
        const cur = this.undo;
        if (cur === null || cur.piece !== piece || cur.ref !== ref) return; // the person went back
        if (!r.ok) {
            this.undo = { ...cur, phase: 'failed', code: r.status === 0 ? 'unreachable' : r.code };
            return;
        }
        const p = parseUndoPreview(r.body);
        // The preview must be for the change and the state the person was offered, or nothing is acted on.
        if (p === null || p.change !== ref.change || p.stagingSeq !== ref.stagingSeq || p.commit !== ref.commit) {
            this.undo = { ...cur, phase: 'failed', code: 'internal' };
            return;
        }
        this.undo = { ...cur, phase: 'preview', preview: p };
    }

    backUndo(): void {
        this.undo = null;
    }

    /** Sends the Undo that the preview showed: the state and the list exactly as the preview gave them. */
    async confirmUndo(): Promise<void> {
        const u = this.undo;
        if (u === null || u.phase !== 'preview' || u.preview === null) return;
        const k = undoKey(u.ref);
        const cur = this.acts[k];
        if (cur && (cur.phase === 'sending' || cur.phase === 'sent' || cur.phase === 'running')) return;
        const p = u.preview;
        await this.send(k, `/api/v1/changes/${encodeURIComponent(p.change)}/undo`, { stagingSeq: p.stagingSeq, commit: p.commit, change: p.change, alsoRemoved: p.alsoRemoved });
    }

    /** The failed state: an alert band, the unknown lamp, an "as of" mark on what is old, and one announcement. */
    private fail(message: string): void {
        if (!this.failed) this.announce = message;
        this.lastSignal = '';
        this.failed = true;
        if (this.floor !== null) this.status = 'ready';
    }

    /** A screen threw while drawing. The page lands in the failed state, never a torn screen. */
    crash(): void {
        queueMicrotask(() => {
            this.crashed = true;
            this.fail('Could not read. This screen could not be drawn.');
        });
    }

    stop(): void {
        this.client?.stop();
        if (this.timer) clearInterval(this.timer);
    }

    private take(f: Floor): void {
        const prev = this.floor;
        const fresh = new Set<string>();
        if (prev !== null && !this.reduceMotion) {
            const was = new Map(prev.pieces.map((p) => [p.id, p.station]));
            for (const p of f.pieces) {
                const s = was.get(p.id);
                if (s !== undefined && s !== p.station) fresh.add(p.id);
            }
        }
        this.fresh = fresh;
        const t = Date.parse(f.generatedAt);
        this.serverMs = Number.isNaN(t) ? 0 : t;
        this.recvPerf = this.env.perfNow();
        this.floor = f;
        this.dropActs(f);
        // The drawer reads its piece again, so it never shows what an earlier state said.
        if (this.drawerId !== null) void this.loadDetail(this.drawerId);
        this.failed = false;
        if (this.crashed) {
            this.crashed = false;
            this.crashEpoch++;
        }
        this.status = 'ready';
        this.tick++;
        // One announcement per change of signal, never one per moving tag.
        const sv = signalView(f, this.nowMs(), false);
        const k = sv.lamp + '|' + sv.word;
        if (k !== this.lastSignal) {
            this.lastSignal = k;
            this.announce = [lampWord(sv.word) + '.', ...sv.parts.map((p) => p.text)].join(' ');
        }
    }

    /** A press is forgotten, and its key with it, once the snapshot no longer has that call at that version. */
    private dropActs(f: Floor): void {
        const live = new Set(f.calls.map(actKey));
        // An Undo press is for one state of the holding area. When the state moves on, the press is over.
        const h = f.holdingArea;
        const undoLive = (k: string) => h !== null && k.startsWith(`u|${h.stagingSeq}|${h.commit}|`);
        for (const k of Object.keys(this.acts)) if (k.startsWith('u|') && undoLive(k)) live.add(k);
        if (this.undo !== null && !undoLive(undoKey(this.undo.ref))) {
            // Only a press that went out says the change was taken out. A refused or failed press says nothing.
            const phase = this.acts[undoKey(this.undo.ref)]?.phase;
            const wasSent = phase === 'sent' || phase === 'running' || phase === 'again';
            this.undo = null;
            if (wasSent) this.announce = 'The change was taken out of the holding area.';
        }
        const keep: Record<string, Act> = {};
        let changed = false;
        for (const [k, v] of Object.entries(this.acts)) {
            if (!live.has(k)) changed = true;
            // A press that was accepted, and a snapshot that still has the call at the same version: the person is not locked out.
            else if (v.phase === 'sent' || v.phase === 'running') {
                keep[k] = { phase: 'again' };
                changed = true;
            } else keep[k] = v;
        }
        for (const k of [...this.idem.keys()]) if (!live.has(k)) this.idem.delete(k);
        if (changed) this.acts = keep;
    }

    go(r: Route): void {
        this.route = r;
        try {
            this.env.location.hash = r;
        } catch {
            /* the address bar stays as it is */
        }
    }

    onHash(): void {
        this.route = parseRoute(this.env.location.hash);
    }

    toggleGoal(n: number): void {
        this.openGoals = { ...this.openGoals, [n]: !this.openGoals[n] };
    }

    openDrawer(id: string, from: HTMLElement | null): void {
        this.opener = from;
        this.drawerId = id;
        this.undo = null;
        void this.loadDetail(id);
    }

    closeDrawer(): void {
        this.drawerId = null;
        this.detail = null;
        this.undo = null;
        const o = this.opener;
        this.opener = null;
        // Focus goes back to the tag that opened the drawer, if it is still there.
        queueMicrotask(() => {
            if (o && o.isConnected) o.focus();
        });
    }
}
