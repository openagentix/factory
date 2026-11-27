<script lang="ts">
    import { ICONS } from './icons';
    let { name }: { name: string } = $props();
    const shapes = $derived(ICONS[name] ?? []);
</script>

<svg class="i" viewBox="0 0 16 16" aria-hidden="true">
    {#each shapes as s, i (i)}
        {#if s.t === 'path'}
            <path d={s.d} fill={s.fill ? 'currentColor' : undefined} />
        {:else if s.t === 'circle'}
            <circle cx={s.cx} cy={s.cy} r={s.r} stroke-dasharray={s.dash} />
        {:else}
            <rect x={s.x} y={s.y} width={s.w} height={s.h} rx={s.rx} />
        {/if}
    {/each}
</svg>
