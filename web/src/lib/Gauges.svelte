<script lang="ts">
    import type { AppState } from './state.svelte';
    import { gaugeView, sparkPoints } from './view';

    let { app }: { app: AppState } = $props();
    const views = $derived(app.floor!.gauges.map(gaugeView));
</script>

<section class="gauges" aria-label="Today">
    {#each views as g (g.key)}
        <div class="g">
            <span class="k">{g.label}</span>
            {#if g.kind === 'value'}
                <span class="v data">{g.value}<small>{g.unit}</small></span>
                {#if g.detail}<span class="d">{g.detail}</span>{/if}
                {#if g.series}
                    {@const sp = sparkPoints(g.series)}
                    {#if sp}
                        <svg class="spark" viewBox="0 0 120 22" preserveAspectRatio="none" role="img" aria-label="Accepted per day, last 7 days: {g.series.join(', ')}">
                            <path d={sp.area} fill="var(--flow-bg)" />
                            <path d={sp.line} fill="none" stroke="var(--flow)" stroke-width="1.6" />
                            <circle cx={sp.last[0]} cy={sp.last[1]} r="2.4" fill="var(--flow)" />
                        </svg>
                    {/if}
                {/if}
            {:else}
                <span class="v honest">{g.honest}</span>
            {/if}
        </div>
    {/each}
</section>
