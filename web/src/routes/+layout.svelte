<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';

	let { children } = $props();
</script>

<svelte:head><link rel="icon" href={favicon} /></svelte:head>

<div class="mx-auto max-w-4xl p-6">
	<header class="mb-6 flex gap-4 border-b pb-3 text-sm">
		<a href="/" class="font-medium hover:underline">Hosts</a>
		<a href="/sessions" class="font-medium hover:underline">Sessions</a>
		<a href="/sessions/new" class="font-medium hover:underline">New session</a>
	</header>
	{@render children()}
</div>
