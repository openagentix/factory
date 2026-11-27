<script lang="ts">
    import Chip from './Chip.svelte';
    let { title, text, status = 'Not built yet' }: { title: string; text: string; status?: string } = $props();
</script>

<div class="head"><div><h1>{title}</h1><p>{text}</p></div></div>
<section class="box">
    <div class="nobuilt"><Chip tone="idle" icon="no" text={status} /><p class="muted">This screen shows no numbers and no sample rows until it is built.</p></div>
</section>
