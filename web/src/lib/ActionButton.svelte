<script lang="ts">
    // A button that does something. While its press is sent it keeps focus and says so with aria-disabled, so a
    // screen reader user is not thrown out of the place they were.
    let { label, kind = '', busy = false, blocked = false, onpress }: { label: string; kind?: '' | 'pri' | 'sm' | 'warn' | 'pri sm'; busy?: boolean; blocked?: boolean; onpress: () => void } = $props();
</script>

<button type="button" class="btn {kind}" aria-disabled={busy || blocked ? 'true' : undefined} onclick={() => { if (!busy && !blocked) onpress(); }}>{label}</button>
