<script lang="ts">
    import type { AppState } from './state.svelte';
    import type { Piece } from './floor';
    import Icon from './Icon.svelte';
    import { isUnreadable } from './text';
    import { pieceAge, pieceLook, pieceName } from './view';

    let { app, p }: { app: AppState; p: Piece } = $props();
    const unreadable = $derived(isUnreadable(p));
    const look = $derived(pieceLook(p, unreadable));
    const right = $derived(p.machine ? p.machine.id : p.station === 7 ? '' : (pieceAge(p, app.nowMs()) ?? ''));
    const other = $derived(p.machine !== null && !p.machine.mine);
</script>

<button
    type="button"
    class="tag {look.tag} {app.fresh.has(p.id) ? 'fresh' : ''}"
    onclick={(e) => app.openDrawer(p.id, e.currentTarget)}
>
    <span class="id"><span>{pieceName(p)}</span><span>{right}</span></span>
    <span class="t">{p.title.text}{p.title.truncated ? '…' : ''}</span>
    {#if p.station < 7}
        <span class="s {look.cls}"><Icon name={look.icon} />{look.word}</span>
    {/if}
    {#if other}<span class="claimed">Claimed by another machine</span>{/if}
</button>
