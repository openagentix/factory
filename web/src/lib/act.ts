// The words around a button press. A refusal sentence is the fixed one for its code (phrases.json, "refusal.<code>").
import { phrase } from './text';
import type { Act } from './state.svelte';

export const MAX_ANSWER_BYTES = 4096;

/** The bytes of an answer as it will be sent: white space at the ends and line ends are not counted differently. */
export function answerBytes(text: string): number {
    return new TextEncoder().encode(text.replace(/\r\n?/g, '\n')).length;
}

export function refusalText(code: string | undefined): string {
    const known = code !== undefined && /^[a-z_]{1,40}$/.test(code);
    try {
        return phrase(known ? 'refusal.' + code : 'refusal.internal');
    } catch {
        return phrase('refusal.internal');
    }
}

/** The one line under a call's buttons. Empty when nothing was pressed. */
export function actMessage(a: Act | undefined): string {
    if (!a) return '';
    switch (a.phase) {
        case 'sending':
            return 'Sending.';
        case 'sent':
            return 'Sent. The line shows it when it is done.';
        case 'running':
            return 'This is being done. The line shows it when it is done.';
        case 'again':
            return 'The call is still there. Press again to send the same press once more.';
        case 'refused':
            return refusalText(a.code);
        default:
            return 'The factory did not answer. Look at the line, then press again if the call is still there.';
    }
}

export const busy = (a: Act | undefined) => a !== undefined && (a.phase === 'sending' || a.phase === 'sent' || a.phase === 'running');

const SEEN = /^[\p{L}\p{N}\p{P}\p{S}]$/u;
const NOT_SEEN = /^[\p{Cc}\p{Cf}\p{White_Space}\p{Variation_Selector}\p{Default_Ignorable_Code_Point}]$/u;
const NEVER_SEEN = new Set([0xfffc, 0xfffd, 0x2800, 0x1d159]);
// What the server refuses wherever it stands: controls other than a line feed and a tab, format characters, line and
// paragraph separators, the Mongolian vowel separator and the replacement character.
const REFUSED = /[\p{Cc}\p{Cf}\u2028\u2029\u180E\uFFFD]/u;

/** True when a person could see a mark of its own in the text: a letter, number, punctuation or symbol. Marks, fillers, joiners and blanks do not count. */
export function hasVisibleText(text: string): boolean {
    for (const ch of text) {
        const cp = ch.codePointAt(0)!;
        if (!NEVER_SEEN.has(cp) && SEEN.test(ch) && !NOT_SEEN.test(ch)) return true;
    }
    return false;
}

/** True when the text holds a character that cannot be sent, whatever stands beside it. Line ends are read as line feeds. */
export function hasRefusedChar(text: string): boolean {
    return REFUSED.test(text.replace(/\r\n?/g, '\n').replace(/[\n\t]/g, ''));
}

/** True when the server would take the text. The same rule as the server's, so the person learns before pressing. */
export function sendable(text: string): boolean {
    return hasVisibleText(text) && !hasRefusedChar(text);
}
