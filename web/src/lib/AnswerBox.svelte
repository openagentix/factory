<script lang="ts">
    import type { Call } from './floor';
    import type { AppState } from './state.svelte';
    import ActionButton from './ActionButton.svelte';
    import { MAX_ANSWER_BYTES, answerBytes, busy as isBusy, refusalText, sendable as canSend } from './act';

    let { app, call }: { app: AppState; call: Call } = $props();
    let value = $state('');
    const act = $derived(app.acts[`${call.id}|${call.version}`]);
    const bytes = $derived(answerBytes(value));
    const over = $derived(bytes > MAX_ANSWER_BYTES);
    const empty = $derived(!canSend(value));
    const refused = $derived(value.trim() !== '' && empty);
    const sendable = $derived((call.actions as string[]).includes('answer'));
</script>

<label class="small muted" for="a-{call.id}">Your answer, in plain words</label>
<textarea id="a-{call.id}" bind:value readonly={isBusy(act)} disabled={!sendable} aria-describedby="n-{call.id}"></textarea>
<p class="small muted" id="n-{call.id}" class:over>{bytes.toLocaleString('en-US')} of {MAX_ANSWER_BYTES.toLocaleString('en-US')}</p>
<p class="small why-no" aria-live="polite">{refused ? refusalText('unprocessable') : ''}</p>
{#if sendable}
    <div class="acts"><ActionButton label="Send answer" kind="pri" busy={isBusy(act)} blocked={empty || over} onpress={() => app.act(call, 'answer', value)} /></div>
{/if}
