<script lang="ts">
    import type { AppState } from './state.svelte';
    import Chip from './Chip.svelte';
    import GoalLines from './GoalLines.svelte';
    import { COURSE_LOOK, UNREAD, lineCounts, miniSummary, pick, pieceLook, stationName, two } from './view';
    import { isUnreadable } from './text';

    let { app }: { app: AppState } = $props();
    const f = $derived(app.floor!);
    const cells = [1, 2, 3, 4, 5, 6, 7];
</script>

<div class="lane hd" aria-hidden="true">
    <span></span>
    <div class="minilab">{#each cells as n (n)}<span>{two(n)}</span>{/each}</div>
    <span></span>
</div>
{#each f.goals as g (g.goal)}
    {@const counts = lineCounts(g)}
    {@const open = !!app.openGoals[g.goal]}
    <div class="lane">
        <div>
            <div class="gn"><span class="ttl">{g.title.text}{g.title.truncated ? '…' : ''}</span></div>
            <div class="lanehead">
                {#if g.course === null}
                    <Chip tone="idle" icon="unk" text="Course: not built yet" />
                {:else}
                    {@const c = pick(COURSE_LOOK, g.course) ?? UNREAD}
                    <Chip tone={c.chip} icon={c.icon} text={c.word} />
                {/if}
                {#if g.lane}<span class="muted small">{g.lane === 'quick' ? 'Quick' : 'Standard'} lane</span>{/if}
            </div>
        </div>
        <div class="mini" role="img" aria-label={miniSummary(f, g.goal)}>
            {#each cells as n (n)}
                <div>
                    {#each f.pieces.filter((p) => p.goal === g.goal && p.station === n) as p (p.id)}
                        <span class="dot {pieceLook(p, isUnreadable(p)).dot}" title="{stationName(n)}"></span>
                    {/each}
                </div>
            {/each}
        </div>
        <div class="lres">
            {#if counts === null}
                <span class="lbn"><span class="chip idle">Lines: not built yet</span></span>
            {:else}
                <span class="lbar" role="img" aria-label="{counts.ok} of {counts.total} lines working">
                    {#each g.lines ?? [] as l (l.id)}<i class={l.state === 'working' ? 'ok' : l.state === 'not_working' ? 'no' : 'unk'}></i>{/each}
                </span>
                <span><b class="data">{counts.ok} of {counts.total}</b> lines working</span>
                <button class="btn sm" type="button" aria-expanded={open} onclick={() => app.toggleGoal(g.goal)}>{open ? 'Hide lines' : 'Show lines'}</button>
            {/if}
        </div>
        {#if open && g.lines !== null}<GoalLines {g} pieces={f.pieces} />{/if}
    </div>
{/each}
{#if f.goals.length === 0}
    <div class="empty">No goals to show.</div>
{/if}
