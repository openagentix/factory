// The key rule (record 0004): the key arrives in the address fragment as t=<key>, is kept in session storage, and
// is removed from the address bar. It is sent only as a header. It never goes in a cookie, a query string or local
// storage. A key from the fragment is only a candidate: it is stored after the server accepts it, so a link from
// someone else cannot replace a good key.

const STORE_NAME = 'oxf-key';
const SHAPE = /^[A-Za-z0-9_-]{16,128}$/;

export type KeyEnv = {
    location: { hash: string; pathname: string; search: string };
    history: { replaceState(data: unknown, unused: string, url?: string | null): void };
    sessionStorage: Pick<Storage, 'getItem' | 'setItem'>;
};

function readStore(env: KeyEnv): string | null {
    try {
        const k = env.sessionStorage.getItem(STORE_NAME);
        return k !== null && SHAPE.test(k) ? k : null;
    } catch {
        return null;
    }
}

/**
 * The fragment is exactly t=<key> or a route name. A fragment that starts with t= is cleared from the address bar at
 * once, whatever it holds. Only the exact form gives a candidate. Nothing is stored here.
 */
export function readKeys(env: KeyEnv): { candidate: string | null; stored: string | null } {
    const frag = env.location.hash.replace(/^#/, '');
    let candidate: string | null = null;
    // A fragment with t= anywhere leaves the address bar at once. Only exactly t=<key> is a candidate.
    if (frag.includes('t=')) {
        const k = frag.startsWith('t=') ? frag.slice(2) : '';
        if (SHAPE.test(k)) candidate = k;
        try {
            env.history.replaceState(null, '', env.location.pathname + env.location.search);
        } catch {
            /* the address bar stays as it is */
        }
    }
    return { candidate, stored: readStore(env) };
}

/** Keeps a key the server has accepted. */
export function rememberKey(env: KeyEnv, key: string): void {
    try {
        env.sessionStorage.setItem(STORE_NAME, key);
    } catch {
        /* the key lives in memory only */
    }
}

/** The headers of an API read. The key is only ever in Authorization. */
export function authHeaders(key: string): Record<string, string> {
    return { Authorization: 'Bearer ' + key, Accept: 'application/json' };
}
