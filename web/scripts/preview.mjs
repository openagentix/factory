// Dev-only static preview of the built app with one fixture as the snapshot. Not part of the build output.
// Usage: node scripts/preview.mjs <fixture.json> <port> [light|dark]
// It listens on 127.0.0.1 only and checks the same bearer key the page is given in its link.
import { createServer } from 'node:http';
import { readFileSync, existsSync, statSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';

const [fixture, portArg, theme] = process.argv.slice(2);
const root = new URL('../dist-app/', import.meta.url).pathname;
const KEY = 'preview-key-0123456789-abcdefghij';
const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2', '.txt': 'text/plain', '.json': 'application/json' };
const snap = readFileSync(fixture);

createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://x');
    if (url.pathname.startsWith('/api/v1/')) {
        if (req.headers.authorization !== 'Bearer ' + KEY) {
            res.writeHead(401).end('{}');
            return;
        }
        if (url.pathname === '/api/v1/floor') {
            res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }).end(snap);
        } else if (url.pathname === '/api/v1/events') {
            res.writeHead(200, { 'Content-Type': 'text/event-stream' });
            res.end('event: hello\ndata: {}\n\n');
        } else res.writeHead(404).end('{}');
        return;
    }
    if (url.pathname === '/theme-init.js' && (theme === 'dark' || theme === 'light')) {
        res.writeHead(200, { 'Content-Type': 'text/javascript' }).end(`document.documentElement.setAttribute('data-theme','${theme}');`);
        return;
    }
    let p = normalize(join(root, decodeURIComponent(url.pathname)));
    if (!p.startsWith(root) || !existsSync(p) || statSync(p).isDirectory()) p = join(root, 'index.html');
    res.writeHead(200, { 'Content-Type': types[extname(p)] ?? 'application/octet-stream' }).end(readFileSync(p));
}).listen(Number(portArg), '127.0.0.1', () => console.log('preview key:', KEY));
