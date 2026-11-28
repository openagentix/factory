<script lang="ts">
    import type { AppState } from './state.svelte';
    import Chip from './Chip.svelte';
    import GoalLines from './GoalLines.svelte';
    import Pending from './Pending.svelte';
    import { COURSE_LOOK, UNREAD, lineCounts, pick } from './view';

    let { app }: { app: AppState } = $props();
    const f = $derived(app.floor!);
</script>

<div class="head sp">
    <div><h1>Goals</h1><p>What the team asked for, in its own words, and whether each line works.</p></div>
    <Pending label="Plan a new goal" kind="pri" />
</div>
{#each f.goals as g (g.goal)}
    {@const counts = lineCounts(g)}
    {@const here = f.pieces.filter((p) => p.goal === g.goal && p.station < 6).length}
    <section class="box">
        <h2><span class="ttl h2t">{g.title.text}{g.title.truncated ? '…' : ''}</span><span>{g.lane === null ? '' : g.lane === 'quick' ? 'Quick lane' : 'Standard lane'}</span></h2>
        <div class="pad gpad">
            {#if g.course === null}
                <Chip tone="idle" icon="unk" text="Course: not built yet" />
            {:else}
                {@const c = pick(COURSE_LOOK, g.course) ?? UNREAD}
                <Chip tone={c.chip} icon={c.icon} text={c.word} />
            {/if}
            {#if counts === null}
                <Chip tone="idle" icon="no" text="Lines: not built yet" />
            {:else}
                <span><b class="data">{counts.ok} of {counts.total}</b> lines working</span>
            {/if}
            <span class="muted">{here} {here === 1 ? 'piece' : 'pieces'} on the line</span>
            <span class="grow"></span>
            <Pending label="Try it" kind="sm" />
            <Pending label="Undo after Accept" kind="sm" />
        </div>
        {#if counts !== null}<div class="pad nopt"><GoalLines {g} pieces={f.pieces} flat /></div>{/if}
    </section>
{/each}
{#if f.goals.length === 0}
    <section class="box"><div class="empty">No goals to show.</div></section>
{/if}
