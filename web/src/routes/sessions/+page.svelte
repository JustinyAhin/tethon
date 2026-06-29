<script lang="ts">
	import { onMount } from 'svelte';
	import { getSessions } from '$lib/remote-functions/sessions.remote';
	import SessionCard from '$lib/components/session-card.svelte';

	const sessions = getSessions();
	const sessionList = $derived(sessions.current?.ok ? sessions.current.sessions : undefined);
	const sessionError = $derived(
		sessions.error?.message ?? (sessions.current?.ok === false ? sessions.current.error : '')
	);

	onMount(() => {
		const timer = setInterval(() => sessions.refresh(), 5000);
		return () => clearInterval(timer);
	});
</script>

<section>
	<div class="mb-4 flex items-center justify-between">
		<h1 class="text-xl font-semibold">Sessions</h1>
		<div class="flex items-center gap-3 text-sm">
			<button class="text-blue-600 hover:underline" onclick={() => sessions.refresh()}>
				Refresh
			</button>
			<a href="/sessions/new" class="text-blue-600 hover:underline">New session</a>
		</div>
	</div>

	{#if sessionError}
		<p class="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			Could not load sessions: {sessionError}
		</p>
	{:else if sessions.loading && !sessions.ready}
		<p class="text-sm text-gray-500">Loading…</p>
	{:else if sessionList && sessionList.length === 0}
		<p class="text-sm text-gray-500">No sessions yet.</p>
	{:else if sessionList}
		<div class="grid gap-3 sm:grid-cols-2">
			{#each sessionList as session (session.id)}
				<SessionCard {session} />
			{/each}
		</div>
	{/if}
</section>
