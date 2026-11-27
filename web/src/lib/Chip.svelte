<script lang="ts">
    import Icon from './Icon.svelte';
    // A chip is an icon and a word, so state is never shown by colour alone.
    let { tone, icon, text }: { tone: string; icon: string; text: string } = $props();
</script>

<span class="chip {tone}"><Icon name={icon} />{text}</span>
