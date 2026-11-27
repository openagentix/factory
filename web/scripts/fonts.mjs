// Copies the font files and their licence texts from the installed packages into static/fonts.
// The files are the publishers' own, unmodified. Nothing is fetched from a font host.
import { copyFileSync, existsSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('../', import.meta.url).pathname;
const out = join(root, 'static', 'fonts');
mkdirSync(out, { recursive: true });
const nm = join(root, 'node_modules');
const plan = [
    ['@ibm/plex-sans/fonts/complete/woff2/IBMPlexSans-Regular.woff2', 'IBMPlexSans-Regular.woff2'],
    ['@ibm/plex-sans/fonts/complete/woff2/IBMPlexSans-Medium.woff2', 'IBMPlexSans-Medium.woff2'],
    ['@ibm/plex-sans/fonts/complete/woff2/IBMPlexSans-SemiBold.woff2', 'IBMPlexSans-SemiBold.woff2'],
    ['@ibm/plex-mono/fonts/complete/woff2/IBMPlexMono-Regular.woff2', 'IBMPlexMono-Regular.woff2'],
    ['@ibm/plex-mono/fonts/complete/woff2/IBMPlexMono-Medium.woff2', 'IBMPlexMono-Medium.woff2'],
    ['@fontsource/barlow-condensed/files/barlow-condensed-latin-500-normal.woff2', 'BarlowCondensed-500.woff2'],
    ['@fontsource/barlow-condensed/files/barlow-condensed-latin-600-normal.woff2', 'BarlowCondensed-600.woff2'],
    ['@fontsource/barlow-condensed/files/barlow-condensed-latin-700-normal.woff2', 'BarlowCondensed-700.woff2'],
    ['@ibm/plex-sans/LICENSE.txt', 'LICENSE-IBM-Plex-Sans.txt'],
    ['@ibm/plex-mono/LICENSE.txt', 'LICENSE-IBM-Plex-Mono.txt'],
    ['@fontsource/barlow-condensed/LICENSE', 'LICENSE-Barlow-Condensed.txt'],
];
for (const [from, to] of plan) {
    const src = join(nm, from);
    if (!existsSync(src)) {
        console.error(`missing font file: ${from}`);
        process.exit(1);
    }
    copyFileSync(src, join(out, to));
}
console.log(`copied ${plan.length} font files`);
