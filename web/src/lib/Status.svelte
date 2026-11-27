<script lang="ts">
    import type { AppState } from './state.svelte';
    import Icon from './Icon.svelte';
    import { lampWord } from './text';
    import { signalView } from './view';

    let { app }: { app: AppState } = $props();
    const f = $derived(app.floor!);
    const sv = $derived(signalView(f, app.nowMs(), app.failed));
    const word = $derived(lampWord(sv.word));
    const icon = $derived({ bad: 'stuck', wait: 'wait', ok: 'work', idle: 'queue', unk: 'unk' }[sv.tone]);
    const cls = $derived({ bad: 'c-bad', wait: 'c-wait', ok: 'c-ok', idle: 'c-idle', unk: 'c-idle' }[sv.tone]);
    const on = (c: string) => (sv.lamp === c ? ' on' : '');
    const asOf = $derived(app.failed ? new Date(Date.parse(f.generatedAt)).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : null);
</script>

<section class="status" aria-label="Line status">
    <div class="tower" role="img" aria-label="Signal: {word}">
        <div class="lamp r{sv.lamp === 'red' ? ' on' : ''}"></div>
        <div class="lamp a{on('amber')}"></div>
        <div class="lamp g{on('green')}"></div>
    </div>
    <div class="say">
        <span class="st {cls}"><Icon name={icon} />{word}</span>
        {#if app.failed}
            <p>The page could not reach the factory. What follows is the last good snapshot, as of {asOf}.</p>
        {:else if sv.parts.length}
            <p>{#each sv.parts as part, i (i)}{i ? ' ' : ''}{#if part.bold}<b class={part.cls}>{part.text}</b>{:else}<span class={part.cls}>{part.text}</span>{/if}{/each}</p>
        {/if}
        {#if f.signal.inputs.paused && !app.failed}<p class="small muted">The line is paused on this machine.</p>{/if}
    </div>
</section>
