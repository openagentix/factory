<script lang="ts">
    import type { AppState } from './state.svelte';
    import Icon from './Icon.svelte';
    import { clockText, journalText } from './text';
    import { LOG_LOOK } from './view';

    let { app, max = 6 }: { app: AppState; max?: number } = $props();
    const rows = $derived(app.floor!.log.slice(0, max));
</script>

{#if rows.length === 0}
    <div class="empty">Nothing has happened yet.</div>
{:else}
    <ul class="log">
        {#each rows as e (e.journalSeq)}
            {@const lk = LOG_LOOK[e.kind] ?? { cls: 'c-idle', icon: 'unk' }}
            <li>
                <time>{clockText(e.time) ?? '--:--'}</time>
                <span class={lk.cls}><Icon name={lk.icon} /></span>
                <span>{journalText(e)}</span>
            </li>
        {/each}
    </ul>
{/if}
