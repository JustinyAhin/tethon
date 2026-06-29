<script lang="ts">
	import type { SessionSummary } from '$lib/types';

	let { session }: { session: SessionSummary } = $props();

	const badge = $derived(
		{
			running: 'bg-green-100 text-green-700',
			awaiting_approval: 'bg-amber-100 text-amber-700',
			error: 'bg-red-100 text-red-700'
		}[session.status] ?? 'bg-gray-100 text-gray-500'
	);
</script>

<a
	href="/sessions/{session.id}"
	class="block rounded-lg border border-gray-200 p-4 hover:bg-gray-50"
>
	<div class="flex items-start justify-between gap-2">
		<div>
			<h3 class="font-medium">{session.repo || session.repo_id || session.id}</h3>
			<p class="text-xs text-gray-500">{session.host_id} · {session.agent}</p>
		</div>
		<span class="rounded-full px-2 py-0.5 text-xs font-medium {badge}">{session.status}</span>
	</div>
	<dl class="mt-3 space-y-1 text-sm text-gray-600">
		<div><span class="text-gray-400">branch:</span> {session.branch || '—'}</div>
		<div><span class="text-gray-400">created:</span> {session.created_at}</div>
	</dl>
</a>
