<script lang="ts">
    import type { AppState } from '../state.svelte';
    import Guard from '../Guard.svelte';
    import Boom from './Boom.svelte';
    let { app }: { app: AppState } = $props();
</script>

<Guard {app}><Boom /></Guard>
