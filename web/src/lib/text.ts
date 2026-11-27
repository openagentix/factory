// Every sentence on the plain screens comes through fill(). It fills numbers and
// codes into a fixed phrase. It refuses anything else, so no title, question or
// other text someone wrote can reach a sentence.
import phrases from './phrases.json';
import type { Call, Fact, JournalEntry, Piece } from './floor';

const table = phrases as Record<string, string>;

const NUMERIC = new Set(['goal', 'ticket', 'attempt', 'maxAttempts', 'count', 'from', 'to', 'moving', 'elsewhere', 'age', 'station', 'total']);
const PIECE_ID = /^[tpk][0-9]{1,9}$/;
const MACHINE_ID = /^[A-Za-z0-9._-]{1,64}$/;
const SLOT = /\{([A-Za-z]+)\}/g;

export type Slots = Record<string, number | string | undefined>;

export class SlotError extends Error {}

/** "t57" is "Ticket 57", "p40" is "Plan of goal 40", "k40" is "Checks of goal 40". */
export function pieceLabel(id: string): string {
    if (!PIECE_ID.test(id)) throw new SlotError(`not a piece id`);
    const n = id.slice(1);
    return id[0] === 't' ? `Ticket ${n}` : id[0] === 'p' ? `Plan of goal ${n}` : `Checks of goal ${n}`;
}

/** Fills the slots of a template. Throws on a string where a number belongs, on an unknown slot, or on an open slot. */
export function fill(template: string, slots: Slots): string {
    return template.replace(SLOT, (_m, name: string) => {
        const v = slots[name];
        if (v === undefined) throw new SlotError(`open slot ${name}`);
        if (name === 'piece' || name === 'blocker') {
            if (typeof v !== 'string') throw new SlotError(`slot ${name} needs a piece id`);
            return pieceLabel(v);
        }
        if (name === 'machine') {
            if (typeof v !== 'string' || !MACHINE_ID.test(v)) throw new SlotError('slot machine needs a machine id');
            return v;
        }
        if (NUMERIC.has(name)) {
            if (typeof v !== 'number' || !Number.isSafeInteger(v) || v < 0) throw new SlotError(`slot ${name} needs a whole number`);
            return String(v);
        }
        throw new SlotError(`unknown slot ${name}`);
    });
}

/** The phrase for a key. A count of exactly 1 uses the ".one" form when there is one. */
export function phrase(key: string, n?: number): string {
    if (n === 1) {
        const one = table[key + '.one'];
        if (one !== undefined) return one;
    }
    const s = table[key];
    if (s === undefined) throw new SlotError(`no phrase ${key}`);
    return s;
}

/** A sentence from a key and slots. Returns null when it cannot be built: the caller then says "Could not read". */
export function sentence(key: string, slots: Slots, n?: number): string | null {
    try {
        return fill(phrase(key, n), slots);
    } catch {
        return null;
    }
}

export const COULD_NOT_READ = table['honest.read_failed'] as string;
export const NOT_BUILT = table['honest.not_built'] as string;
export const NOT_ENOUGH = table['honest.not_enough_yet'] as string;

export function lampWord(word: string): string {
    return table['lamp.' + word] ?? COULD_NOT_READ;
}

function whole(v: Fact | undefined): number | undefined {
    return typeof v === 'number' ? v : undefined;
}

/** The sentence for a piece's reason. A reason with no phrase, or with an open slot, reads "Could not read". */
export function reasonText(p: Piece): string {
    if (!p.reason) return COULD_NOT_READ;
    const r = p.reason;
    const n = r.code === 'attempts_used_up' ? r.maxAttempts : r.count;
    const s = sentence(
        'reason.' + r.code,
        { piece: p.id, blocker: p.blockedBy ?? undefined, attempt: r.attempt, maxAttempts: r.maxAttempts, count: r.count, machine: p.machine?.id },
        n,
    );
    return s ?? COULD_NOT_READ;
}

/** True when the reason says the factory could not tell: such a piece is never drawn as moving. */
export function isUnreadable(p: Piece): boolean {
    return p.reason !== null && (p.reason.code === 'unknown' || p.reason.code === 'read_failed');
}

export function journalText(e: JournalEntry): string {
    const f = e.facts ?? {};
    const s = sentence('journal.' + e.kind, { goal: e.goal, ticket: e.ticket, from: whole(f.from), to: whole(f.to) });
    return s ?? (table['journal.unreadable'] as string);
}

export function callText(c: Call): string | null {
    const f = c.facts;
    switch (c.kind) {
        case 'question':
            return c.ticket === null ? null : sentence('call.question', { piece: 't' + c.ticket });
        case 'consequence': {
            const risk = f.risk;
            return typeof risk === 'string' && /^[a-z_]{1,20}$/.test(risk) ? sentence('call.consequence.' + risk, {}) : null;
        }
        case 'plan':
            return sentence('call.plan', { goal: c.goal ?? whole(f.goal) });
        case 'accept': {
            const n = whole(f.count);
            return n === undefined ? null : sentence('call.accept', { count: n }, n);
        }
        case 'statement':
            return sentence('call.statement', {});
    }
    return null;
}

// ---- time ----

/** Minutes as the mockup writes them: "40 min", "1 h 05 min". Under a minute is "under 1 min". */
export function ageText(ms: number): string {
    const m = Math.floor(Math.max(0, ms) / 60000);
    if (m < 1) return 'under 1 min';
    if (m < 60) return `${m} min`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h} h ${m % 60 < 10 ? '0' : ''}${m % 60} min`;
    return `${Math.floor(h / 24)} d ${h % 24} h`;
}

export function clockText(iso: string | null): string | null {
    if (iso === null) return null;
    const t = Date.parse(iso);
    if (Number.isNaN(t)) return null;
    return new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
