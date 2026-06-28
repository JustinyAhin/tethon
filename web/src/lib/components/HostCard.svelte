<script lang="ts">
	import type { HostSummary } from '$lib/types';

	let { host }: { host: HostSummary } = $props();

	const repoNames = $derived(host.repos?.map((r) => r.name) ?? host.repo_roots);
</script>

<div class="rounded-lg border border-gray-200 p-4">
	<div class="flex items-start justify-between gap-2">
		<div>
			<h3 class="font-medium">{host.name}</h3>
			<p class="text-xs text-gray-500">{host.host_id}</p>
		</div>
		<span
			class="rounded-full px-2 py-0.5 text-xs font-medium {host.online
				? 'bg-green-100 text-green-700'
				: 'bg-gray-100 text-gray-500'}"
		>
			{host.online ? 'online' : 'offline'}
		</span>
	</div>

	<dl class="mt-3 space-y-1 text-sm text-gray-600">
		<div><span class="text-gray-400">agents:</span> {host.agents.join(', ') || '—'}</div>
		<div><span class="text-gray-400">repos:</span> {repoNames.join(', ') || '—'}</div>
		<div><span class="text-gray-400">last seen:</span> {host.last_seen}</div>
	</dl>

	{#if host.online}
		<a
			href="/sessions/new?host={host.host_id}"
			class="mt-3 inline-block text-sm font-medium text-blue-600 hover:underline"
		>
			Start session →
		</a>
	{/if}
</div>
