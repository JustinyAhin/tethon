<script lang="ts">
	import { onMount } from 'svelte';
	import { browser } from '$app/env';
	import { page } from '$app/state';

	import type { SessionSummary } from '$lib/types';

	import { getSessions } from '$lib/remote-functions/sessions.remote';
	import LegacySession from '$lib/components/legacy-session.svelte';
	import CodexSession from '$lib/components/codex-session.svelte';

	const sessions = getSessions();

	let cached = $state<SessionSummary>();

	const id = $derived(`${page.params.host}/${page.params.local}`);
	const session = $derived(
		(sessions.current?.ok ? sessions.current.sessions.find((s) => s.id === id) : undefined) ??
			(cached?.id === id ? cached : undefined)
	);

	$effect(() => {
		if (!browser || !session) return;
		try {
			localStorage.setItem(`tethon:session:${id}`, JSON.stringify(session));
		} catch {
			/* Keep the live route usable. */
		}
	});

	onMount(() => {
		try {
			cached = JSON.parse(localStorage.getItem(`tethon:session:${id}`) || 'null') ?? undefined;
		} catch {
			/* Reload from the host. */
		}
		void sessions.refresh();
	});
</script>

<svelte:head>
	<title>{session?.agent === 'codex-cli' ? 'Codex conversation' : 'Session'} · Tethon</title>
</svelte:head>

{#key id}
	{#if session?.agent === 'codex-cli'}
		<CodexSession {session} />
	{:else if session}
		<LegacySession />
	{:else if sessions.loading}
		<p role="status" class="text-sm text-gray-500">Loading session…</p>
	{:else}
		<p role="alert" class="text-sm text-red-700">
			{sessions.current?.ok === false
				? sessions.current.error
				: 'This session is not available on the host.'}
		</p>
		<button class="mt-3 cursor-pointer text-sm text-blue-700" onclick={() => sessions.refresh()}
			>Try again</button
		>
	{/if}
{/key}
