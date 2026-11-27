<script lang="ts">
    import Shell from '../lib/Shell.svelte';
    import type { Env } from '../lib/state.svelte';

    const env: Env = {
        fetch: (...a) => window.fetch(...a),
        location: window.location,
        history: window.history,
        sessionStorage: window.sessionStorage,
        localStorage: window.localStorage,
        perfNow: () => performance.now(),
        matchMedia: (q) => window.matchMedia(q),
    };
</script>

<Shell {env} />
