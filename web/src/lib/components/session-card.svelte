<script lang="ts">
	import type { SessionSummary } from '$lib/types';

	import { label } from '$lib/codex';

	let { session }: { session: SessionSummary } = $props();

	const status = $derived.by(() => {
		if (session.agent !== 'codex-cli' || !session.codex) return session.status;

		const native = session.codex;
		if (native.conversation_state !== 'ready') return native.conversation_state;
		if (native.connection_state !== 'connected') return native.connection_state;
		return native.turn_state === 'in_progress' ? 'working' : native.turn_state;
	});
	const badge = $derived(
		{
			running: 'bg-green-100 text-green-700',
			working: 'bg-green-100 text-green-700',
			awaiting_approval: 'bg-amber-100 text-amber-700',
			error: 'bg-red-100 text-red-700',
			failed: 'bg-red-100 text-red-700'
		}[status] ?? 'bg-gray-100 text-gray-600'
	);
</script>

<a
	href="/sessions/{session.id}"
	class="block rounded-lg border border-gray-200 p-4 hover:bg-gray-50"
>
	<div class="flex items-start justify-between gap-2">
		<div class="min-w-0">
			<h3 class="break-words font-medium">{session.repo || session.repo_id || session.id}</h3>
			<p class="text-xs text-gray-500">{session.host_id} · {session.agent}</p>
		</div>
		<span class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium {badge}"
			>{label(status)}</span
		>
	</div>

	<dl class="mt-3 space-y-1 text-sm text-gray-600">
		<div><span class="text-gray-500">Branch:</span> {session.branch || '—'}</div>
		<div><span class="text-gray-500">Created:</span> {session.created_at}</div>
	</dl>
</a>
