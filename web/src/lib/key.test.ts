import { describe, expect, it } from 'vitest';
import { authHeaders, readKeys, rememberKey, type KeyEnv } from './key';

const KEY = 'abcDEF0123456789_-abcDEF0123456789_-xyzXYZ';
const OTHER = 'ATTACKERKEY0123456789abcdefghij';

function env(hash: string, stored: string | null = null) {
    const store = new Map<string, string>();
    if (stored) store.set('oxf-key', stored);
    const urls: string[] = [];
    const e: KeyEnv = {
        location: { hash, pathname: '/', search: '' },
        history: { replaceState: (_d, _u, url) => void urls.push(String(url)) },
        sessionStorage: { getItem: (k) => store.get(k) ?? null, setItem: (k, v) => void store.set(k, v) },
    };
    return { e, store, urls };
}

describe('the key', () => {
    it('a fragment key is only a candidate: it does not overwrite a stored key, and leaves the address bar at once', () => {
        const { e, store, urls } = env('#t=' + OTHER, KEY);
        expect(readKeys(e)).toEqual({ candidate: OTHER, stored: KEY });
        expect(store.get('oxf-key')).toBe(KEY);
        expect(urls).toEqual(['/']);
        expect(urls.join('')).not.toContain(OTHER);
    });
    it('is stored only when remembered after the server accepted it', () => {
        const { e, store } = env('#t=' + KEY);
        expect(readKeys(e)).toEqual({ candidate: KEY, stored: null });
        expect(store.size).toBe(0);
        rememberKey(e, KEY);
        expect(store.get('oxf-key')).toBe(KEY);
    });
    it('the fragment is exactly t=<key> or a route name: t=KEY&goals is no key and is cleared', () => {
        const a = env('#t=' + KEY + '&goals');
        expect(readKeys(a.e)).toEqual({ candidate: null, stored: null });
        expect(a.urls).toEqual(['/']);
        const b = env('#goals', KEY);
        expect(readKeys(b.e)).toEqual({ candidate: null, stored: KEY });
        expect(b.urls).toEqual([]);
    });
    it('a bad key in the fragment is dropped, the stored key stays, and the bar is cleared', () => {
        const { e, store, urls } = env('#t=short', KEY);
        expect(readKeys(e)).toEqual({ candidate: null, stored: KEY });
        expect(store.get('oxf-key')).toBe(KEY);
        expect(urls).toEqual(['/']);
    });
    it('is nothing when there is none, and survives a storage that throws', () => {
        expect(readKeys(env('').e)).toEqual({ candidate: null, stored: null });
        const { e } = env('#t=' + KEY);
        e.sessionStorage = { getItem: () => { throw new Error('no'); }, setItem: () => { throw new Error('no'); } };
        expect(readKeys(e)).toEqual({ candidate: KEY, stored: null });
        expect(() => rememberKey(e, KEY)).not.toThrow();
    });
    it('travels only in the Authorization header', () => {
        const h = authHeaders(KEY);
        expect(h.Authorization).toBe('Bearer ' + KEY);
        expect(Object.values(h).filter((v) => v.includes(KEY))).toHaveLength(1);
    });
});
