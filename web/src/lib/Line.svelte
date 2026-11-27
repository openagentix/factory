<script lang="ts">
    import type { AppState } from './state.svelte';
    import Bay from './Bay.svelte';
    import Icon from './Icon.svelte';
    import Pending from './Pending.svelte';
    import { beltMoves } from './view';

    let { app }: { app: AppState } = $props();
    const f = $derived(app.floor!);
    // The belt moves only when something is moving on this machine, the line is not paused, and the data is fresh.
    const halt = $derived(!beltMoves(f, app.failed));
</script>

<section class="linewrap {app.failed ? 'stale' : ''}" aria-label="The line">
    <div class="linehead">
        <h2>The line</h2>
        <div class="legend">
            <span class="c-ok"><Icon name="work" />Working</span>
            <span class="c-idle"><Icon name="queue" />Waiting for a machine</span>
            <span class="c-wait"><Icon name="wait" />Waiting for a person</span>
            <span class="c-bad"><Icon name="stuck" />Stuck</span>
        </div>
        <Pending label="Pause the line" kind="sm" />
    </div>
    <div class="scroll" role="region" aria-label="The line, stations 01 to 07">
        <div class="line">
            {#each f.stations as st (st.no)}<Bay {app} {st} />{/each}
        </div>
        <div class="belt{halt ? ' halt' : ''}" aria-hidden="true"><i></i></div>
    </div>
</section>
