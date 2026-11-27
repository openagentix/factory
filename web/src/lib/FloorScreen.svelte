<script lang="ts">
    import type { AppState } from './state.svelte';
    import Calls from './Calls.svelte';
    import Gauges from './Gauges.svelte';
    import Icon from './Icon.svelte';
    import Lanes from './Lanes.svelte';
    import Line from './Line.svelte';
    import Log from './Log.svelte';
    import Status from './Status.svelte';

    let { app }: { app: AppState } = $props();
    const f = $derived(app.floor!);
</script>

<div class="head">
    <div><h1>Factory floor</h1><p>Every piece of work, where it is on the line, and what stops it.</p></div>
    {#if f.repository}<span class="repo"><Icon name="bot" />{f.repository}{f.mode === 'demo' ? ' (fake)' : ''}</span>{/if}
</div>
<Status {app} />
<Gauges {app} />
<Line {app} />
<div class="cols">
    <div class="goalcol">
        <section class="box"><h2>Goals on the line<span>position shows progress</span></h2><Lanes {app} /></section>
        <section class="box"><h2>Since you last looked<span>latest first</span></h2><Log {app} /></section>
    </div>
    <section class="box"><h2>Needs a person<span>{f.calls.length} open</span></h2><Calls {app} max={3} /></section>
</div>
