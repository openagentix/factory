<script lang="ts">
    import { onMount } from 'svelte';
    import type { AppState } from './state.svelte';
    import Icon from './Icon.svelte';
    import Pending from './Pending.svelte';
    import { isUnreadable, reasonText } from './text';
    import { goalById, pieceById, pieceAge, pieceLook, stationName, two } from './view';
    import { COULD_NOT_READ, NOT_BUILT, clockText } from './text';

    let { app }: { app: AppState } = $props();
    const f = $derived(app.floor!);
    const p = $derived(pieceById(f, app.drawerId));
    const look = $derived(p ? pieceLook(p, isUnreadable(p)) : null);
    const goal = $derived(p ? goalById(f, p.goal) : null);
    let root: HTMLElement;

    onMount(() => {
        const first = root.querySelector<HTMLElement>('button');
        first?.focus();
    });

    function focusables(): HTMLElement[] {
        return [...root.querySelectorAll<HTMLElement>('button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')];
    }

    function key(e: KeyboardEvent) {
        if (e.key === 'Escape') {
            e.preventDefault();
            e.stopPropagation();
            app.closeDrawer();
            return;
        }
        if (e.key !== 'Tab') return;
        const els = focusables();
        if (els.length === 0) return;
        const first = els[0]!;
        const last = els[els.length - 1]!;
        const active = document.activeElement;
        if (e.shiftKey && (active === first || !root.contains(active))) {
            e.preventDefault();
            last.focus();
        } else if (!e.shiftKey && (active === last || !root.contains(active))) {
            e.preventDefault();
            first.focus();
        }
    }
</script>

<svelte:window onkeydown={key} />

<div class="scrim" role="presentation" onclick={() => app.closeDrawer()}></div>
<div
    bind:this={root}
    class="drawer{app.failed ? ' stale' : ''}"
    role="dialog"
    aria-modal="true"
    aria-label={p ? `${p.kind === 'ticket' && p.ticket !== null ? 'Ticket ' + p.ticket : p.kind === 'plan' ? 'Plan of goal ' + p.goal : 'Checks of goal ' + p.goal}` : 'Piece'}
>
    <div class="dhead">
        <div>
            {#if p}<span class="data muted">{p.kind === 'ticket' && p.ticket !== null ? 'Ticket #' + p.ticket : p.kind === 'plan' ? 'Plan, goal ' + p.goal : 'Checks, goal ' + p.goal}</span>
                <h2><span class="ttl">{p.title.text}{p.title.truncated ? '…' : ''}</span></h2>{/if}
        </div>
        <button class="btn sm" type="button" onclick={() => app.closeDrawer()}>Close</button>
    </div>
    {#if app.failed}<p class="asof">Last good snapshot, as of {clockText(f.generatedAt) ?? COULD_NOT_READ}. Everything here is old.</p>{/if}
    {#if p && look}
        <div class="dchips">
            <span class="chip {look.chip}"><Icon name={look.icon} />{p.station === 7 ? 'Accepted' : look.word}</span>
            <span class="data small">at {two(p.station)} {stationName(p.station)}</span>
            {#if p.machine}<span class="data small">on {p.machine.id}</span>{/if}
            {#if goal}<span class="muted small">for: <span class="ttl">{goal.title.text}</span></span>{/if}
            {#if pieceAge(p, app.nowMs())}<span class="muted small">here {pieceAge(p, app.nowMs())}</span>{/if}
        </div>
        {#if p.reason}<p class="dreason {look.cls}">{reasonText(p)}</p>{/if}
        <div><h3>Its route so far</h3><p class="muted">{NOT_BUILT}</p></div>
        <div><h3>Decided for you</h3><p class="muted">{NOT_BUILT}</p></div>
        <div><h3>Could not be confirmed</h3><p class="muted">{NOT_BUILT}</p></div>
        <div class="acts">
            {#if p.station === 6}<Pending label="Undo this change" />{/if}
        </div>
    {:else}
        <p class="muted">This piece is no longer on the line.</p>
    {/if}
</div>
