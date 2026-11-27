import { describe, expect, it } from 'vitest';
import { ageText, callText, fill, isUnreadable, journalText, lampWord, phrase, pieceLabel, reasonText, sentence, SlotError } from './text';
import type { Call, Piece } from './floor';

const piece = (over: Partial<Piece>): Piece => ({
    id: 't57', kind: 'ticket', goal: 42, ticket: 57, title: { text: 'x', truncated: false }, station: 4, state: 'stuck',
    reason: { code: 'check_failed', attempt: 2, maxAttempts: 3 }, since: null, blockedBy: null, rootBlocker: null,
    machine: null, claim: null, run: null, call: null, ...over,
});

describe('fill', () => {
    it('fills numbers and a piece id', () => {
        expect(fill('{piece}: try {attempt} of {maxAttempts}.', { piece: 't57', attempt: 2, maxAttempts: 3 })).toBe('Ticket 57: try 2 of 3.');
    });
    it('refuses a string where a number belongs', () => {
        expect(() => fill('Goal {goal}: accepted.', { goal: '<script>' })).toThrow(SlotError);
        expect(() => fill('Goal {goal}: accepted.', { goal: '41' })).toThrow(SlotError);
    });
    it('refuses a title in a piece slot', () => {
        expect(() => fill('{piece}: x', { piece: 'Fix the <b>bug</b>' })).toThrow(SlotError);
    });
    it('refuses a machine id with markup, an unknown slot, an open slot and a negative or fractional number', () => {
        expect(() => fill('by {machine}', { machine: 'a b<' })).toThrow(SlotError);
        expect(() => fill('{title}', { title: 'x' })).toThrow(SlotError);
        expect(() => fill('{count}', {})).toThrow(SlotError);
        expect(() => fill('{count}', { count: -1 })).toThrow(SlotError);
        expect(() => fill('{count}', { count: 1.5 })).toThrow(SlotError);
    });
    it('labels the three kinds of piece', () => {
        expect(pieceLabel('p40')).toBe('Plan of goal 40');
        expect(pieceLabel('k40')).toBe('Checks of goal 40');
        expect(() => pieceLabel('x40')).toThrow(SlotError);
    });
});

describe('phrases and plural forms', () => {
    it('uses the one form for exactly 1', () => {
        expect(phrase('call.accept', 1)).toBe('1 change is in the holding area. Accept it, or ask a developer to finish this step.');
        expect(phrase('call.accept', 2)).toContain('{count} changes');
        expect(phrase('call.accept', 0)).toContain('{count} changes');
        expect(phrase('reason.queued', 1)).toBe(phrase('reason.queued', 2));
    });
    it('builds the signal parts', () => {
        expect(sentence('signal.moving', { moving: 2 })).toBe('2 moving.');
        expect(sentence('signal.elsewhere', { elsewhere: 1 })).toBe('1 claimed by another machine.');
    });
    it('returns null instead of a sentence with an open slot', () => {
        expect(sentence('signal.moving', {})).toBeNull();
        expect(sentence('no.such.key', {})).toBeNull();
    });
    it('renders singular and plural reasons', () => {
        expect(reasonText(piece({ reason: { code: 'too_many_cut_offs', count: 1 } }))).toBe('Ticket 57: it was cut off 1 time.');
        expect(reasonText(piece({ reason: { code: 'too_many_cut_offs', count: 3 } }))).toBe('Ticket 57: it was cut off 3 times.');
        expect(reasonText(piece({ reason: { code: 'attempts_used_up', maxAttempts: 1 } }))).toBe('Ticket 57: the 1 try is used.');
        expect(reasonText(piece({ reason: { code: 'attempts_used_up', maxAttempts: 3 } }))).toBe('Ticket 57: all 3 tries are used.');
    });
    it('renders a reason phrase for a code, and "Could not read" for a code it does not know or a missing slot', () => {
        expect(reasonText(piece({}))).toBe('Ticket 57: the checks failed. Try 2 of 3.');
        expect(reasonText(piece({ reason: { code: 'made_up_code' } }))).toBe('Could not read');
        expect(reasonText(piece({ reason: { code: 'check_failed' } }))).toBe('Could not read');
        expect(reasonText(piece({ reason: null }))).toBe('Could not read');
        expect(reasonText(piece({ id: 'p44', reason: { code: 'plan_waits' } }))).toBe('Plan of goal 44: the plan waits for you.');
    });
    it('names the blocker and the machine', () => {
        expect(reasonText(piece({ reason: { code: 'waiting_dependency' }, blockedBy: 't52' }))).toBe('Ticket 57: waiting for Ticket 52.');
        expect(reasonText(piece({ reason: { code: 'claim_expired' }, machine: { id: 'laptop-b', mine: false } }))).toBe('Ticket 57: the claim by laptop-b ran out. It waits for a machine.');
    });
    it('knows an unreadable piece', () => {
        expect(isUnreadable(piece({ reason: { code: 'unknown' } }))).toBe(true);
        expect(isUnreadable(piece({ reason: { code: 'read_failed' } }))).toBe(true);
        expect(isUnreadable(piece({}))).toBe(false);
    });
});

describe('lamp words', () => {
    it('has all six words, and an unknown word reads Could not read', () => {
        expect(['could_not_read', 'stuck', 'needs_a_person', 'running', 'paused', 'idle'].map(lampWord)).toEqual([
            'Could not read', 'Stuck', 'Needs a person', 'Running', 'Paused', 'Idle',
        ]);
        expect(lampWord('green')).toBe('Could not read');
    });
});

describe('calls and journal', () => {
    const call = (over: Partial<Call>): Call => ({ id: 'c', kind: 'plan', who: 'anyone_trusted', since: null, goal: 44, ticket: null, question: null, facts: {}, actions: [], version: 'v', ...over });
    it('builds each kind from fixed phrases', () => {
        expect(callText(call({}))).toBe('Goal 44: read the lines and approve the plan.');
        expect(callText(call({ kind: 'question', ticket: 58 }))).toBe('Ticket 58: the agent asked a question.');
        expect(callText(call({ kind: 'consequence', facts: { risk: 'security' } }))).toBe('This changes who may do what in your product. Is that what you want?');
        expect(callText(call({ kind: 'accept', facts: { count: 1 } }))).toContain('1 change is');
        expect(callText(call({ kind: 'accept', facts: { count: 3 } }))).toContain('3 changes are');
        expect(callText(call({ kind: 'accept', facts: {} }))).toBeNull();
        expect(callText(call({ kind: 'consequence', facts: { risk: '<b>' } }))).toBeNull();
    });
    it('never puts a hostile fact into a sentence', () => {
        expect(journalText({ journalSeq: 1, time: null, kind: 'accepted', goal: '<img>' as unknown as number })).toBe('One entry could not be read.');
        expect(journalText({ journalSeq: 1, time: null, kind: 'made_up' })).toBe('One entry could not be read.');
        expect(journalText({ journalSeq: 1, time: null, kind: 'dial_moved', facts: { from: 1, to: 2 } })).toBe('The level of a kind of work moved from 1 to 2.');
    });
});

describe('ages', () => {
    it('writes minutes, hours and days', () => {
        expect(ageText(10_000)).toBe('under 1 min');
        expect(ageText(40 * 60_000)).toBe('40 min');
        expect(ageText(65 * 60_000)).toBe('1 h 05 min');
        expect(ageText(25 * 3600_000)).toBe('1 d 1 h');
        expect(ageText(-5)).toBe('under 1 min');
    });
});
