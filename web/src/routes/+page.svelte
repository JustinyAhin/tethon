<script lang="ts">
	import { getHosts } from '$lib/remote-functions/hosts.remote';
	import HostCard from '$lib/components/host-card.svelte';

	const hosts = getHosts();
</script>

<section>
	<div class="mb-4 flex items-center justify-between">
		<h1 class="text-xl font-semibold">Hosts</h1>
		<button class="text-sm text-blue-600 hover:underline" onclick={() => hosts.refresh()}>
			Refresh
		</button>
	</div>

	{#if hosts.error}
		<p class="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			Could not load hosts: {hosts.error.message}
		</p>
	{:else if hosts.loading && !hosts.ready}
		<p class="text-sm text-gray-500">Loading…</p>
	{:else if hosts.current && hosts.current.length === 0}
		<p class="text-sm text-gray-500">No hosts registered yet.</p>
	{:else if hosts.current}
		<div class="grid gap-3 sm:grid-cols-2">
			{#each hosts.current as host (host.host_id)}
				<HostCard {host} />
			{/each}
		</div>
	{/if}
</section>
