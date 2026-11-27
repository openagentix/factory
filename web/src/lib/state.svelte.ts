// The state of the page: the key, the latest snapshot, the clock, the route and the open drawer.
import { startClient, type Client } from './client';
import type { Floor } from './floor';
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
    announce = $state('');
    /** Ids of pieces whose station changed between the last two snapshots. */
    fresh = $state.raw<ReadonlySet<string>>(new Set());
    reduceMotion = false;

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
    }

    closeDrawer(): void {
        this.drawerId = null;
        const o = this.opener;
        this.opener = null;
        // Focus goes back to the tag that opened the drawer, if it is still there.
        queueMicrotask(() => {
            if (o && o.isConnected) o.focus();
        });
    }
}
