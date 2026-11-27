<script lang="ts">
    import type { Snippet } from 'svelte';
    import type { AppState } from './state.svelte';
    import { COULD_NOT_READ } from './text';

    // A screen that throws while it draws lands in the failed state with a fixed message.
    let { app, children }: { app: AppState; children: Snippet } = $props();
</script>

<svelte:boundary onerror={() => app.crash()}>
    {@render children()}
    {#snippet failed()}
        <section class="box" role="alert"><div class="empty"><p>{COULD_NOT_READ}. This screen could not be drawn.</p></div></section>
    {/snippet}
</svelte:boundary>
