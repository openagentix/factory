<script lang="ts">
    import type { AppState } from './state.svelte';
    import Chip from './Chip.svelte';
    import type { Call } from './floor';
    import Icon from './Icon.svelte';
    import Pending from './Pending.svelte';
    import { COULD_NOT_READ, ageText, callText } from './text';
    import { CALL_KIND_LOOK, WHO_WORD, acceptGoals, callTitle } from './view';

    let { app, max }: { app: AppState; max?: number } = $props();
    const f = $derived(app.floor!);
    const list = $derived(max === undefined ? f.calls : f.calls.slice(0, max));
    const unknown = $derived(f.signal.lamp === 'unknown');
    const goals = $derived(acceptGoals(f));
    const has = (c: Call, a: string) => (c.actions as string[]).includes(a);
    function waiting(c: Call): string | null {
        if (c.since === null) return null;
        const t = Date.parse(c.since);
        return Number.isNaN(t) ? null : ageText(app.nowMs() - t);
    }
</script>

{#if f.calls.length === 0}
    <div class="empty">
        <Icon name={unknown ? 'unk' : 'ok'} />
        <p>{unknown ? 'Could not read what waits for a person.' : 'Nothing waits for a person.'}</p>
    </div>
{/if}
{#each list as c (c.id)}
    {@const k = CALL_KIND_LOOK[c.kind]}
    {@const text = callText(c)}
    {@const title = callTitle(f, c)}
    {@const w = waiting(c)}
    <div class="call" data-call={c.id}>
        <div class="meta">
            <Chip tone={k.chip} icon={k.icon} text={k.word} />
            <span><b>{WHO_WORD[c.who]}</b> must act</span>
            {#if w}<span class="data">waiting {w}</span>{/if}
        </div>
        <p class="q">{text ?? COULD_NOT_READ}</p>
        {#if title}<p class="small muted">About: <span class="ttl">{title}</span></p>{/if}
        {#if c.kind === 'question' && c.question}
            <div class="quote"><span class="by">Written by the agent, as written:</span><span class="tx">{c.question.text}{c.question.truncated ? ' (cut short)' : ''}</span></div>
            <label class="small muted" for="a-{c.id}">Your answer, in plain words</label>
            <textarea id="a-{c.id}" disabled></textarea>
        {/if}
        {#if c.kind === 'accept' && goals.length}
            <ul class="acc-goals" aria-label="What is in the holding area">
                {#each goals as g (g.goal)}
                    <li>Goal {g.goal}{#if g.title !== null}: <span class="ttl">{g.title}</span>{/if} ({g.count} {g.count === 1 ? 'change' : 'changes'})</li>
                {/each}
            </ul>
        {/if}
        <div class="acts">
            {#if c.kind === 'question' && has(c, 'answer')}<Pending label="Send answer" kind="pri" />{/if}
            {#if c.kind === 'consequence'}
                {#if has(c, 'yes')}<Pending label="Yes, that is what I want" kind="pri" />{/if}
                {#if has(c, 'no')}<Pending label="No, send it back" />{/if}
            {/if}
            {#if c.kind === 'plan'}
                {#if has(c, 'approve')}<Pending label="Approve the plan" kind="pri" />{/if}
                <Pending label="Keep planning" />
            {/if}
            {#if c.kind === 'accept'}
                <Pending label="Try it" />
                {#if has(c, 'accept')}<Pending label="Accept" kind="pri" />{/if}
                {#if has(c, 'cancel')}<Pending label="Cancel the Accept" />{/if}
                <Pending label="Undo a change" />
            {/if}
        </div>
        {#if c.kind === 'accept'}
            <p class="caution"><Icon name="stuck" />Try it runs code the factory wrote. No person has read it.</p>
        {/if}
    </div>
{/each}
{#if max !== undefined && f.calls.length > max}
    <div class="pad"><button class="btn sm" type="button" onclick={() => app.go('needs-you')}>See all {f.calls.length}</button></div>
{/if}
