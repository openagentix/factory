<script lang="ts">
    import type { AppState } from './state.svelte';
    import Calls from './Calls.svelte';

    let { app }: { app: AppState } = $props();
</script>

<div class="head"><div><h1>Needs you</h1><p>The only places the factory stops for a person. One plain question each. Everything else keeps moving.</p></div></div>
<section class="box"><h2>Open<span>oldest first</span></h2><Calls {app} /></section>
