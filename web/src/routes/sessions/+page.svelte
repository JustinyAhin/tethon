<script lang="ts">
	import { getSessions } from '$lib/remote-functions/sessions.remote';
	import SessionCard from '$lib/components/session-card.svelte';

	const sessions = getSessions();
</script>

<section>
	<div class="mb-4 flex items-center justify-between">
		<h1 class="text-xl font-semibold">Sessions</h1>
		<a href="/sessions/new" class="text-sm text-blue-600 hover:underline">New session</a>
	</div>

	{#if sessions.error}
		<p class="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			Could not load sessions: {sessions.error.message}
		</p>
	{:else if sessions.loading && !sessions.ready}
		<p class="text-sm text-gray-500">Loading…</p>
	{:else if sessions.current && sessions.current.length === 0}
		<p class="text-sm text-gray-500">No sessions yet.</p>
	{:else if sessions.current}
		<div class="grid gap-3 sm:grid-cols-2">
			{#each sessions.current as session (session.id)}
				<SessionCard {session} />
			{/each}
		</div>
	{/if}
</section>
