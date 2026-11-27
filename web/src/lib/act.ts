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
        case 'refused':
            return refusalText(a.code);
        default:
            return 'The factory did not answer. Look at the line, then press again if the call is still there.';
    }
}

export const busy = (a: Act | undefined) => a !== undefined && (a.phase === 'sending' || a.phase === 'sent');

const SHOWS_SOMETHING = /[^\p{Cc}\p{Cf}\p{White_Space}\p{Variation_Selector}\p{Default_Ignorable_Code_Point}\u2800]/u;

/** True when a person could see something in the text. Fillers, joiners, variation selectors and blank patterns show nothing. */
export function hasVisibleText(text: string): boolean {
    return SHOWS_SOMETHING.test(text);
}
