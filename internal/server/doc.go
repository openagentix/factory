// Package server is the local HTTP server of the web app (record 0004).
//
// It listens on 127.0.0.1 only. Every request goes through the same guards,
// in one fixed order, before any route runs: Host, Origin, fetch site, the
// key for /api/, the method, and for writes the content type, the size and a
// strict JSON decode. All routes live in one table that only the guarded
// dispatcher reads, and routes are fixed before serving starts.
//
// # Limits of the server itself
//
// A local process can still spend the connection cap by connecting again and
// again. The cap is 64 open connections. A connection that is let in is cut
// off by the header timeout (5 seconds), the read deadline for the whole
// request (10 seconds) and the write timeout for a response (10 seconds).
// A refused request closes its connection at once. Even so, a process that
// reconnects without pause can keep most of the slots busy. That is a local
// denial of service this server cannot fully prevent. The person with the
// key is still served in turn, because a connection beyond the cap is not
// accepted until a slot frees, so it waits in the kernel queue in order
// instead of being turned away. If that queue fills, the kernel refuses new
// connections. This is a choice: accepting and closing at once would turn the
// person away for as long as the attacker holds the slots.
//
// Go's own HTTP code answers some requests before any handler runs: a request
// line or header it cannot parse (400), a header block that is too large
// (431), an unknown protocol version (505) and an unknown transfer encoding
// (501). Those answers do not carry the security headers. We accept this and
// do not write our own parser. They hold fixed text, repeat no request bytes
// and carry no Access-Control header. A test pins exactly which answers lack
// the headers. "OPTIONS *" is not one of them: it reaches the guards and is
// answered 405 (or 421).
//
// The key is in memory only. An agent running as the same user can still find
// it in the link file, the terminal or the browser (record 0004, Limits).
package server
