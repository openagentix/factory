<script lang="ts">
    import type { AppState } from './state.svelte';
    import type { Station } from './floor';
    import Icon from './Icon.svelte';
    import Tag from './Tag.svelte';
    import { COULD_NOT_READ, NOT_BUILT, reasonText } from './text';
    import { GATES, bayClass, oldestAt, pieceById, piecesAt, stationName, two } from './view';

    let { app, st }: { app: AppState; st: Station } = $props();
    const f = $derived(app.floor!);
    const here = $derived(piecesAt(f, st.no));
    const shown = $derived(here.slice(0, 4));
    const notBuilt = $derived(st.state === 'not_built' || st.source === 'not_built');
    const countText = $derived(
        notBuilt || st.count === null ? null : `${st.count} ${st.no === 7 ? 'today' : st.count === 1 ? 'piece' : 'pieces'}`,
    );
    const oldest = $derived(st.no < 7 && st.count ? oldestAt(f, st.no, app.nowMs()) : null);

    type Why = { cls: string; icon: string; text: string } | null;
    const why = $derived<Why>(compute());
    function compute(): Why {
        if (notBuilt) return { cls: 'c-idle', icon: 'no', text: NOT_BUILT };
        switch (st.state) {
            case 'unknown': return { cls: 'c-idle', icon: 'unk', text: COULD_NOT_READ };
            case 'stuck': {
                const p = pieceById(f, st.oldestStuck) ?? here.find((x) => x.state === 'stuck') ?? null;
                return { cls: 'c-bad', icon: 'stuck', text: p ? reasonText(p) : COULD_NOT_READ };
            }
            case 'waiting_person': {
                const p = here.find((x) => x.state === 'waiting_person');
                return { cls: 'c-wait', icon: 'wait', text: p ? reasonText(p) : COULD_NOT_READ };
            }
            case 'working': {
                const mine = here.some((x) => x.state === 'working' && x.machine?.mine !== false);
                return { cls: 'c-ok', icon: 'work', text: mine ? 'Moving' : 'Claimed by another machine' };
            }
            case 'waiting_machine': {
                const p = here.find((x) => x.state === 'waiting_machine');
                return { cls: 'c-idle', icon: 'queue', text: p ? reasonText(p) : COULD_NOT_READ };
            }
            case 'ready':
                return st.no === 6
                    ? { cls: 'c-flow', icon: 'done', text: 'Checked. Ready to accept.' }
                    : { cls: 'c-flow', icon: 'done', text: 'Part of the product' };
        }
        return null;
    }
    const who = $derived(st.who === 'person' ? { icon: 'wait', text: GATES[st.no - 1] ?? '' } : st.who === 'agent' ? { icon: 'bot', text: 'An agent works here' } : { icon: 'ok', text: 'The factory checks here' });
</script>

<div class="bay {bayClass(st)}" data-station={st.no}>
    <div class="hz"></div>
    <div class="bh">
        <span class="no">{two(st.no)}</span>
        <span class="nm">{stationName(st.no)}
            {#if st.source === 'scripted'}<span class="stn">scripted</span>{:else if st.source === 'partly_real'}<span class="stn">partly real</span>{/if}
        </span>
        <span class="who"><Icon name={who.icon} />{who.text}</span>
    </div>
    <div class="bstat">
        <div class="top">
            {#if countText !== null}<span class="cnt">{countText}</span>{:else}<span class="cnt">&nbsp;</span>{/if}
            {#if oldest}<span class="muted">oldest {oldest}</span>{/if}
        </div>
        {#if why}<span class="why {why.cls}"><Icon name={why.icon} />{why.text}</span>{/if}
    </div>
    <div class="tags">
        {#each shown as p (p.id)}<Tag {app} {p} />{/each}
        {#if here.length > 4}<span class="more">and {here.length - 4} more</span>{/if}
    </div>
</div>
