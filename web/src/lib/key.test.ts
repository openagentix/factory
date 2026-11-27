import { describe, expect, it } from 'vitest';
import { authHeaders, loadKey, type KeyEnv } from './key';

const KEY = 'abcDEF0123456789_-abcDEF0123456789_-xyzXYZ';

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
    it('is taken from the fragment, kept in session storage, and removed from the address bar', () => {
        const { e, store, urls } = env('#t=' + KEY);
        expect(loadKey(e)).toBe(KEY);
        expect(store.get('oxf-key')).toBe(KEY);
        expect(urls).toEqual(['/']);
        expect(urls.join('')).not.toContain(KEY);
    });
    it('keeps another part of the fragment, such as the screen', () => {
        const { e, urls } = env('#goals&t=' + KEY);
        expect(loadKey(e)).toBe(KEY);
        expect(urls).toEqual(['/#goals']);
    });
    it('comes from session storage when the fragment has none', () => {
        const { e, urls } = env('#goals', KEY);
        expect(loadKey(e)).toBe(KEY);
        expect(urls).toEqual([]);
    });
    it('is null when there is none, or when the shape is wrong', () => {
        expect(loadKey(env('').e)).toBeNull();
        expect(loadKey(env('#floor').e)).toBeNull();
        const bad = env('#t=short!');
        expect(loadKey(bad.e)).toBeNull();
        expect(bad.store.size).toBe(0);
        expect(bad.urls).toEqual(['/']);
    });
    it('survives a storage that throws', () => {
        const { e } = env('#t=' + KEY);
        e.sessionStorage = { getItem: () => { throw new Error('no'); }, setItem: () => { throw new Error('no'); } };
        expect(loadKey(e)).toBe(KEY);
    });
    it('travels only in the Authorization header', () => {
        const h = authHeaders(KEY);
        expect(h.Authorization).toBe('Bearer ' + KEY);
        expect(Object.values(h).filter((v) => v.includes(KEY))).toHaveLength(1);
    });
});
