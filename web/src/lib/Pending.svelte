<script lang="ts">
    // An action that is not built yet: shown, disabled, with the words in view and not only in a tooltip.
    let { label, kind = '' }: { label: string; kind?: '' | 'pri' | 'sm' | 'warn' | 'pri sm' } = $props();
    const id = `nb-${Math.random().toString(36).slice(2, 8)}`;
</script>

<span class="nbtn"><button type="button" class="btn {kind}" disabled aria-describedby={id}>{label}</button><small class="nb" {id}>Not built yet</small></span>
