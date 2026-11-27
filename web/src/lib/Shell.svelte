<script lang="ts">
    import { onMount } from 'svelte';
    import { AppState, ROUTES, type Env, type Route } from './state.svelte';
    import Drawer from './Drawer.svelte';
    import FloorScreen from './FloorScreen.svelte';
    import GoalsScreen from './GoalsScreen.svelte';
    import Icon from './Icon.svelte';
    import NeedsScreen from './NeedsScreen.svelte';
    import NotBuilt from './NotBuilt.svelte';
    import Guard from './Guard.svelte';
    import { COULD_NOT_READ, clockText } from './text';
    import { modeKind } from './view';

    let { env }: { env: Env } = $props();
    // The state is made once. The environment is fixed for the life of the page.
    // svelte-ignore state_referenced_locally
    const app = new AppState(env);

    const NAV: { group: string; note?: string; items: { r: Route; label: string; icon: string }[] }[] = [
        { group: 'Factory', items: [
            { r: 'floor', label: 'Floor', icon: 'floor' }, { r: 'needs-you', label: 'Needs you', icon: 'bell' },
            { r: 'goals', label: 'Goals', icon: 'goals' }, { r: 'live', label: 'Live', icon: 'live' }, { r: 'team', label: 'Team', icon: 'team' } ] },
        { group: 'Engineering', items: [{ r: 'runs', label: 'Runs and changes', icon: 'runs' }, { r: 'autonomy', label: 'Autonomy', icon: 'autonomy' }] },
        { group: 'Organisation', note: 'designed', items: [{ r: 'plant', label: 'Plant', icon: 'plant' }, { r: 'record', label: 'Record', icon: 'record' }] },
    ];

    const NOT_BUILT_SCREENS: Partial<Record<Route, { title: string; text: string; status?: string }>> = {
        live: { title: 'Live', text: 'What each machine is doing right now: its job, its step, and why other work waits.' },
        team: { title: 'Team', text: 'Who is trusted to start work or answer, and which machine each person runs. Each person signs in to their own agent on their own machine.' },
        runs: { title: 'Runs and changes', text: 'The same work with the machinery showing: each run, its checks, and the changes it made. This screen uses engineering words.' },
        autonomy: { title: 'Autonomy', text: 'One dial per kind of job. It sets where the factory stops for a person once a plan is approved.' },
        plant: { title: 'Plant', text: 'Every line in the organisation at once, so the eye goes to the lines that need someone.', status: 'Designed, not built' },
        record: { title: 'Record', text: 'What the factory produced, whether it stayed good, and what it used.', status: 'Designed, not built' },
    };

    onMount(() => {
        app.start();
        const sync = () => app.onHash();
        window.addEventListener('hashchange', sync);
        return () => {
            window.removeEventListener('hashchange', sync);
            app.stop();
        };
    });

    $effect(() => {
        document.title = (app.demo ? 'DEMO ' : '') + 'Factory Floor';
    });

    function toggleTheme() {
        const r = document.documentElement;
        let d = r.getAttribute('data-theme');
        if (d !== 'light' && d !== 'dark') d = env.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
        d = d === 'dark' ? 'light' : 'dark';
        r.setAttribute('data-theme', d);
        try {
            env.localStorage.setItem('oxf-theme', d);
        } catch {
            /* the choice is not remembered */
        }
    }

    const f = $derived(app.floor);
    const badge = $derived(f !== null && f.calls.length > 0 ? f.calls.length : null);
    const dataScreen = $derived(app.route === 'floor' || app.route === 'needs-you' || app.route === 'goals');
</script>

{#if app.status === 'nokey' || app.status === 'denied'}
    <main class="nograph">
        <h1>Factory</h1>
        <p>This page needs the link printed when the program started.</p>
    </main>
{:else}
    <div class="app" inert={app.drawerId !== null}>
        <aside class="rail">
            <div class="brand">
                <svg viewBox="0 0 30 30" aria-hidden="true"><path d="M2 27V13l8 5v-5l8 5V6h8v21z" fill="currentColor" /><path d="M7 22h3M13 22h3M19 22h4" stroke="var(--bg)" stroke-width="2" /></svg>
                <div><b>Factory</b><span>OpenAgentix Software Factory</span></div>
            </div>
            {#if app.demo}<div class="demoplate">Demo</div>{/if}
            {#each NAV as g (g.group)}
                <nav class="navg" aria-label={g.group}>
                    <h3>{g.group}{#if g.note} <em>{g.note}</em>{/if}</h3>
                    {#each g.items as it (it.r)}
                        <button class="nav" type="button" aria-current={app.route === it.r ? 'page' : undefined} onclick={() => app.go(it.r)}>
                            <Icon name={it.icon} />{it.label}
                            {#if it.r === 'needs-you' && badge !== null}<span class="n">{badge}</span>{/if}
                        </button>
                    {/each}
                </nav>
            {/each}
            <div class="railfoot">
                <button class="tgl" type="button" onclick={toggleTheme}><Icon name="sun" />Light / dark</button>
            </div>
        </aside>

        <main id="main">
            <div class="screen">
                {#if app.demo}
                    <div class="band" role="note">DEMO. A made-up project on a fake GitHub. The factory code that moves this line is real, except stations marked scripted.</div>
                {:else if f !== null && modeKind(f.mode) !== 'live'}
                    <div class="band warn" role="note">{COULD_NOT_READ}. The factory could not tell whether this is a demo or a real project.</div>
                {/if}
                {#if app.failed}
                    <div class="band warn" role="alert">{COULD_NOT_READ}. The page could not {app.crashed ? 'draw this screen' : 'read a good snapshot from the factory'}. It tries again by itself.</div>
                {/if}

                {#key app.crashEpoch}
                    <Guard {app}>
                        {#if dataScreen && f === null}
                            {#if app.failed}
                                <section class="box"><div class="empty"><p>{COULD_NOT_READ}. The factory did not answer. This page tries again by itself.</p></div></section>
                            {:else}
                                <section class="box"><div class="empty"><p>Reading the factory.</p></div></section>
                            {/if}
                        {:else if dataScreen && f !== null}
                            <div class={app.failed ? 'stale' : ''}>
                                {#if app.failed}<p class="asof">Last good snapshot, as of {clockText(f.generatedAt) ?? COULD_NOT_READ}. Everything below is old.</p>{/if}
                                {#if app.route === 'floor'}<FloorScreen {app} />{:else if app.route === 'needs-you'}<NeedsScreen {app} />{:else}<GoalsScreen {app} />{/if}
                            </div>
                        {:else}
                            {@const n = NOT_BUILT_SCREENS[app.route]}
                            {#if n}<NotBuilt title={n.title} text={n.text} status={n.status} />{/if}
                        {/if}
                    </Guard>
                {/key}
            </div>
        </main>
    </div>
    {#if app.drawerId !== null && f !== null}<Drawer {app} />{/if}
    <div class="sr" role="status" aria-live="polite">{app.announce}</div>
{/if}
