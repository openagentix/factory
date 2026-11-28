<script lang="ts">
    import type { AppState } from './state.svelte';
    import Chip from './Chip.svelte';
    import type { Call } from './floor';
    import Icon from './Icon.svelte';
    import Pending from './Pending.svelte';
    import ActionButton from './ActionButton.svelte';
    import AnswerBox from './AnswerBox.svelte';
    import { tick } from 'svelte';
    import { actMessage, busy } from './act';
    import { enter } from './focus';
    import { COULD_NOT_READ, ageText, callText } from './text';
    import { CALL_KIND_LOOK, UNREAD, WHO_WORD, acceptGoals, callTitle, pick } from './view';

    let { app, max }: { app: AppState; max?: number } = $props();
    const f = $derived(app.floor!);
    const list = $derived(max === undefined ? f.calls : f.calls.slice(0, max));
    const unknown = $derived(f.signal.lamp === 'unknown');
    const goals = $derived(acceptGoals(f));
    const has = (c: Call, a: string) => (c.actions as string[]).includes(a);
    const actOf = (c: Call) => app.acts[`${c.id}|${c.version}`];
    // Accept asks once. The question is for one call at one version: a new version closes it.
    let confirming = $state<string | null>(null);
    const ckey = (c: Call) => `${c.id}|${c.version}`;
    $effect(() => {
        if (confirming !== null && !f.calls.some((c) => ckey(c) === confirming)) confirming = null;
    });
    async function back(c: Call): Promise<void> {
        confirming = null;
        await tick();
        root?.querySelector<HTMLElement>(`[data-call="${CSS.escape(c.id)}"] [data-act="accept"]`)?.focus();
    }
    const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
    // Focus. When a later snapshot removes the call that held focus, focus goes to the next call's sentence, or to the list.
    let root: HTMLElement | undefined = $state();
    let focusId: string | null = null;
    let order: string[] = [];
    $effect(() => {
        const ids = list.map((c) => c.id);
        const before = order;
        order = ids;
        if (focusId !== null && !ids.includes(focusId)) {
            const gone = focusId;
            focusId = null;
            const after = before.slice(before.indexOf(gone) + 1).find((id) => ids.includes(id)) ?? ids[0];
            queueMicrotask(() => {
                const el = after === undefined ? null : root?.querySelector<HTMLElement>(`[data-call="${CSS.escape(after)}"] .q`);
                (el ?? root)?.focus();
            });
        }
    });
    function waiting(c: Call): string | null {
        if (c.since === null) return null;
        const t = Date.parse(c.since);
        return Number.isNaN(t) ? null : ageText(app.nowMs() - t);
    }
</script>

<div class="calls" tabindex="-1" bind:this={root}>
{#if f.calls.length === 0}
    <div class="empty">
        <Icon name={unknown ? 'unk' : 'ok'} />
        <p>{unknown ? 'Could not read what waits for a person.' : 'Nothing waits for a person.'}</p>
    </div>
{/if}
{#each list as c (c.id)}
    {@const k = pick(CALL_KIND_LOOK, c.kind) ?? UNREAD}
    {@const text = callText(c)}
    {@const title = callTitle(f, c)}
    {@const w = waiting(c)}
    <div
        class="call"
        data-call={c.id}
        onfocusin={() => (focusId = c.id)}
        onfocusout={(e) => {
            const to = e.relatedTarget as Node | null;
            if (to !== null && !e.currentTarget.contains(to)) focusId = null;
        }}
    >
        <div class="meta">
            <Chip tone={k.chip} icon={k.icon} text={k.word} />
            <span><b>{pick(WHO_WORD, c.who) ?? COULD_NOT_READ}</b> must act</span>
            {#if w}<span class="data">waiting {w}</span>{/if}
        </div>
        <p class="q" tabindex="-1">{text ?? COULD_NOT_READ}</p>
        {#if title}<p class="small muted">About: <span class="ttl">{title}</span></p>{/if}
        {#if c.kind === 'question' && c.question}
            <div class="quote"><span class="by">Written by the agent, as written:</span><span class="tx">{c.question.text}{c.question.truncated ? ' (cut short)' : ''}</span></div>
            <AnswerBox {app} call={c} />
        {/if}
        {#if c.kind === 'accept' && goals.length}
            <ul class="acc-goals" aria-label="What is in the holding area">
                {#each goals as g (g.goal)}
                    <li>Goal {g.goal}{#if g.title !== null}: <span class="ttl">{g.title}</span>{/if} ({g.count} {g.count === 1 ? 'change' : 'changes'})</li>
                {/each}
            </ul>
        {/if}
        <div class="acts">
            {#if c.kind === 'consequence'}
                {#if has(c, 'yes')}<ActionButton label="Yes, that is what I want" kind="pri" busy={busy(actOf(c))} onpress={() => app.act(c, 'yes')} />{/if}
                {#if has(c, 'no')}<ActionButton label="No, send it back" busy={busy(actOf(c))} onpress={() => app.act(c, 'no')} />{/if}
            {/if}
            {#if c.kind === 'plan'}
                {#if has(c, 'approve')}<ActionButton label="Approve the plan" kind="pri" busy={busy(actOf(c))} onpress={() => app.act(c, 'approve')} />{/if}
                <Pending label="Keep planning" />
            {/if}
            {#if c.kind === 'accept'}
                <Pending label="Try it" />
                {#if has(c, 'accept')}<ActionButton label="Accept" kind="pri" tag="accept" busy={busy(actOf(c))} onpress={() => (confirming = ckey(c))} />{/if}
                {#if has(c, 'cancel')}<ActionButton label="Cancel the Accept" tag="cancel" busy={busy(actOf(c))} onpress={() => app.act(c, 'cancel')} />{/if}
            {/if}
        </div>
        {#if c.kind === 'accept' && confirming === ckey(c) && has(c, 'accept')}
            <!-- Escape goes back from the question. The keys are only for people who use a keyboard; the Back button does the same. -->
            <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
            <div class="confirm" role="group" aria-labelledby="cf-{c.id}" onkeydown={(e) => { if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); void back(c); } }}>
                <p class="q" id="cf-{c.id}" tabindex="-1" use:enter>Accept everything in the holding area: {plural(goals.reduce((n, g) => n + g.count, 0), 'change', 'changes')} in {plural(goals.length, 'goal', 'goals')}. This puts it into the product.</p>
                <div class="acts">
                    <ActionButton label="Accept now" kind="pri" busy={busy(actOf(c))} onpress={() => { confirming = null; void app.act(c, 'accept'); }} />
                    <button type="button" class="btn" onclick={() => back(c)}>Back</button>
                </div>
            </div>
        {/if}
        {#if c.kind === 'accept'}<p class="small muted">To take a change back, open it on the line.</p>{/if}
        <p class="small act-msg" aria-live="polite">{actMessage(actOf(c))}</p>
        {#if c.kind === 'accept'}
            <p class="caution"><Icon name="stuck" />Try it runs code the factory wrote. No person has read it.</p>
        {/if}
    </div>
{/each}
{#if max !== undefined && f.calls.length > max}
    <div class="pad"><button class="btn sm" type="button" onclick={() => app.go('needs-you')}>See all {f.calls.length}</button></div>
{/if}
</div>
