import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { svelteTesting } from '@testing-library/svelte/vite';
import { defineConfig } from 'vitest/config';
import type { Plugin } from 'vite';

// SvelteKit's root component writes its route announcer with an inline style attribute. The server's policy
// has no inline styles. This swaps the attribute for a class, and fails loudly when SvelteKit changes the markup.
function announcerWithoutInlineStyle(): Plugin {
    const attr = /\s+style="position: absolute; left: 0;[^"]*"/;
    return {
        name: 'announcer-without-inline-style',
        enforce: 'pre',
        transform(code, id) {
            if (
                !id.endsWith('.svelte-kit/generated/root.svelte') &&
                !id.endsWith('@sveltejs/kit/src/runtime/components/root.svelte')
            )
                return null;
            if (!attr.test(code)) throw new Error('SvelteKit changed its route announcer markup: update vite.config.ts');
            return code.replace(attr, ' class="announcer"');
        },
    };
}

export default defineConfig({
    plugins: [
        announcerWithoutInlineStyle(),
        sveltekit({
            compilerOptions: { runes: true },
            adapter: adapter({ pages: 'dist-app', assets: 'dist-app', fallback: 'index.html', precompress: false, strict: true }),
            // The policy has no inline styles, so no stylesheet may be put into the page.
            inlineStyleThreshold: 0,
        }),
        svelteTesting(),
    ],
    test: {
        environment: 'jsdom',
        include: ['src/**/*.test.ts'],
        setupFiles: ['./src/test-setup.ts'],
        globals: false,
        css: false,
    },
});
