// The server's replies for one piece and for an undo preview are untrusted too. Each parser accepts a reply only
// when every field has the right type and a known value, and returns null otherwise: the page then says "Could not
// read" and never shows half of a reply.
import type { PieceDetail, UndoPreview } from './floor';
import { Bad, CALL_ID, MACHINE, MAX_TITLE, MODES, PIECE_ID, PIECE_KINDS, PIECE_STATES, RUN_ID, arr, int, nullable, obj, ok, oneOf, reason, str, time, untrusted } from './parse';

const ENTRY_ID = /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/;
const COMMIT40 = /^[0-9a-f]{40}$/;
const MAX_LISTED = 50;
const MAX_ITEMS = 20;
const MAX_ROUTE = 7;

export function parseDetail(input: unknown): PieceDetail | null {
    try {
        const d = obj(input);
        ok(d.schemaVersion === 1);
        oneOf(d.mode, MODES);
        time(d.generatedAt);
        const p = obj(d.piece);
        str(p.id, PIECE_ID);
        oneOf(p.kind, PIECE_KINDS);
        int(p.goal);
        nullable(p.ticket, (x) => int(x));
        untrusted(p.title, MAX_TITLE);
        ok(Number.isSafeInteger(p.station) && (p.station as number) >= 1 && (p.station as number) <= 7);
        oneOf(p.state, PIECE_STATES);
        nullable(p.reason, reason);
        nullable(p.since, time);
        for (const k of ['blockedBy', 'rootBlocker']) nullable(p[k], (x) => str(x, PIECE_ID));
        nullable(p.machine, (x) => {
            const m = obj(x);
            str(m.id, MACHINE);
            ok(typeof m.mine === 'boolean');
        });
        nullable(p.claim, (x) => {
            const c = obj(x);
            time(c.since);
            time(c.expires);
        });
        nullable(p.run, (x) => str(x, RUN_ID));
        nullable(p.call, (x) => str(x, CALL_ID));
        const seen = new Set<number>();
        for (const r of arr(d.route, MAX_ROUTE).map(obj)) {
            ok(Number.isSafeInteger(r.station) && (r.station as number) >= 1 && (r.station as number) <= 7);
            ok(!seen.has(r.station as number));
            seen.add(r.station as number);
            nullable(r.at, time);
        }
        for (const k of ['rulings', 'unconfirmed']) for (const u of arr(d[k], MAX_ITEMS)) untrusted(u, 300);
        nullable(d.undo, (x) => {
            const u = obj(x);
            str(u.change, ENTRY_ID);
            ok(int(u.stagingSeq, Number.MAX_SAFE_INTEGER) >= 1);
            str(u.commit, COMMIT40);
        });
        return input as PieceDetail;
    } catch (e) {
        if (e instanceof Bad) return null;
        throw e;
    }
}

/**
 * An undo preview is accepted only when it is whole: the change and the entries that leave with it are plain ids, the
 * tickets list has the change first and then one entry per id in `alsoRemoved`, in the same order, and no id repeats.
 */
export function parseUndoPreview(input: unknown): UndoPreview | null {
    try {
        const d = obj(input);
        oneOf(d.mode, MODES);
        const change = str(d.change, ENTRY_ID);
        ok(int(d.stagingSeq, Number.MAX_SAFE_INTEGER) >= 1);
        str(d.commit, COMMIT40);
        const also = arr(d.alsoRemoved, MAX_LISTED).map((x) => str(x, ENTRY_ID));
        const tickets = arr(d.tickets, MAX_LISTED + 1).map(obj);
        ok(tickets.length === also.length + 1);
        const ids = [change, ...also];
        ok(new Set(ids).size === ids.length);
        tickets.forEach((t, i) => {
            ok(t.change === ids[i]);
            ok(int(t.ticket) >= 1);
        });
        return input as UndoPreview;
    } catch (e) {
        if (e instanceof Bad) return null;
        throw e;
    }
}
