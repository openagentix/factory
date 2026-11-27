// The key rule (record 0004): the key arrives in the address fragment as t=<key>,
// moves to session storage, and is removed from the address bar. It is sent only
// as a header. It never goes in a cookie, a query string or local storage.

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

/** Takes the key from the fragment (and removes it there), else from session storage. Null when there is none. */
export function loadKey(env: KeyEnv): string | null {
    const frag = env.location.hash.replace(/^#/, '');
    if (frag !== '') {
        const parts = frag.split('&');
        const rest: string[] = [];
        let found: string | null = null;
        for (const p of parts) {
            if (p.startsWith('t=')) found = p.slice(2);
            else rest.push(p);
        }
        if (found !== null || parts.some((p) => p.startsWith('t='))) {
            // The fragment is cleared in every case, even when the key is no good.
            const next = rest.join('&');
            try {
                env.history.replaceState(null, '', env.location.pathname + env.location.search + (next ? '#' + next : ''));
            } catch {
                /* the address bar stays as it is */
            }
            if (found !== null && SHAPE.test(found)) {
                try {
                    env.sessionStorage.setItem(STORE_NAME, found);
                } catch {
                    /* the key lives in memory only */
                }
                return found;
            }
            return readStore(env);
        }
    }
    return readStore(env);
}

/** The headers of an API read. The key is only ever in Authorization. */
export function authHeaders(key: string): Record<string, string> {
    return { Authorization: 'Bearer ' + key, Accept: 'application/json' };
}
