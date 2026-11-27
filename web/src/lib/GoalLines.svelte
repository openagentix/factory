<script lang="ts">
    import Chip from './Chip.svelte';
    import type { Goal, Piece } from './floor';
    import { reasonText, sentence } from './text';
    import { LINE_LOOK } from './view';

    let { g, pieces, flat = false }: { g: Goal; pieces: Piece[]; flat?: boolean } = $props();
    // A line's note is a fixed phrase for its code. It needs a ticket to name; without one there is no note.
    function note(l: NonNullable<Goal['lines']>[number]): string | null {
        if (!l.note || l.note.ticket === undefined) return null;
        // A phrase with try numbers needs the piece itself, which holds them.
        const p = pieces.find((x) => x.ticket === l.note!.ticket && x.reason?.code === l.note!.code);
        if (p) return reasonText(p);
        return sentence('reason.' + l.note.code, { piece: 't' + l.note.ticket });
    }
</script>

<div class="lineswrap{flat ? ' flat' : ''}">
<p class="linesnote">The lines, in the person's own words</p>
<ul class="lines">
    {#each g.lines ?? [] as l (l.id)}
        {@const lk = LINE_LOOK[l.state]}
        <li>
            <Chip tone={lk.chip} icon={lk.icon} text={lk.word} />
            <span>
                <span class="lt">{l.text.text}{l.text.truncated ? '…' : ''}</span>
                {#if note(l)}<span class="note">{note(l)}</span>{/if}
            </span>
        </li>
    {/each}
</ul>
</div>
