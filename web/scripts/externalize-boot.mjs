// SvelteKit writes a small inline <script> into index.html that starts the app. The Content-Security-Policy
// has no 'unsafe-inline' and no script hashes, so this step moves that script into a file next to the other assets and
// points the page at it. It also fails the build if any inline script or style is left, so the strict policy holds.
import { createHash } from 'node:crypto';
import { readFileSync, readdirSync, statSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const dir = new URL('../dist-app/', import.meta.url).pathname;
const file = join(dir, 'index.html');
let html = readFileSync(file, 'utf8');

const inline = /<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g;
let n = 0;
html = html.replace(inline, (_m, body) => {
    n += 1;
    const hash = createHash('sha256').update(body).digest('hex').slice(0, 10);
    const name = `_app/immutable/boot.${hash}.js`;
    writeFileSync(join(dir, name), body.trim() + '\n');
    return `<script src="/${name}"></script>`;
});

const left =
    [...html.matchAll(inline)].length +
    [...html.matchAll(/<style[\s>]/g)].length +
    [...html.matchAll(/\sstyle="/g)].length;
if (left > 0) {
    console.error('index.html still has inline script or style: the CSP would block it');
    process.exit(1);
}
writeFileSync(file, html);

// other pages the static adapter wrote (none with a SPA fallback, but be safe)
function walk(d) {
    for (const f of readdirSync(d)) {
        const p = join(d, f);
        if (statSync(p).isDirectory()) walk(p);
        else if (p.endsWith('.html') && p !== file) {
            const t = readFileSync(p, 'utf8');
            if (/<script(?![^>]*\bsrc=)[^>]*>[\s\S]*?<\/script>/.test(t)) {
                console.error(`${p} has an inline script`);
                process.exit(1);
            }
        }
    }
}
walk(dir);
console.log(`externalized ${n} inline script(s) from index.html`);
