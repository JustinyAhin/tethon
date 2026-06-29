<script lang="ts">
	import { page } from '$app/state';
	import { PersistedState, watch } from 'runed';
	import { getHosts } from '$lib/remote-functions/hosts.remote';
	import { createSession } from '$lib/remote-functions/sessions.remote';

	const hosts = getHosts();

	const lastHost = new PersistedState('tethon:lastHost', '');
	let selectedHostId = $state(page.url.searchParams.get('host') ?? lastHost.current);
	let useRawRepo = $state(false);

	watch(
		() => selectedHostId,
		(id) => {
			lastHost.current = id;
		}
	);

	const hostList = $derived(hosts.current?.ok ? hosts.current.hosts : []);
	const hostError = $derived(
		hosts.error?.message ?? (hosts.current?.ok === false ? hosts.current.error : '')
	);
	const onlineHosts = $derived(hostList.filter((h) => h.online));
	const selectedHost = $derived(onlineHosts.find((h) => h.host_id === selectedHostId));
	const repos = $derived(selectedHost?.repos ?? []);
	const agents = $derived(selectedHost?.agents ?? []);
	const rawRepo = $derived(useRawRepo || repos.length === 0);
</script>

<section class="max-w-xl">
	<h1 class="mb-4 text-xl font-semibold">Start a session</h1>

	{#if hostError}
		<p class="mb-4 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			Could not load hosts: {hostError}
		</p>
	{/if}

	<form {...createSession} class="space-y-4">
		<label class="block">
			<span class="text-sm font-medium">Host</span>
			<select name="host_id" bind:value={selectedHostId} class="mt-1 w-full rounded border p-2">
				<option value="" disabled>Select a host…</option>
				{#each onlineHosts as h (h.host_id)}
					<option value={h.host_id}>{h.name} ({h.host_id})</option>
				{/each}
			</select>
			{#each createSession.fields.host_id.issues() ?? [] as issue (issue.message)}
				<span class="mt-1 block text-xs text-red-600">{issue.message}</span>
			{/each}
		</label>

		{#if rawRepo}
			<label class="block">
				<span class="text-sm font-medium">Repo path</span>
				<input name="repo" placeholder="/abs/path/to/repo" class="mt-1 w-full rounded border p-2" />
			</label>
		{:else}
			<label class="block">
				<span class="text-sm font-medium">Repo</span>
				<select name="repo_id" class="mt-1 w-full rounded border p-2">
					<option value="" disabled selected>Select a repo…</option>
					{#each repos as r (r.id)}
						<option value={r.id}>{r.name}</option>
					{/each}
				</select>
			</label>
			<label class="flex items-center gap-2 text-sm text-gray-600">
				<input type="checkbox" bind:checked={useRawRepo} /> Enter a raw path instead
			</label>
		{/if}
		{#each createSession.fields.repo_id.issues() ?? [] as issue (issue.message)}
			<span class="block text-xs text-red-600">{issue.message}</span>
		{/each}

		<label class="block">
			<span class="text-sm font-medium">Agent</span>
			<select name="agent" class="mt-1 w-full rounded border p-2">
				<option value="">default (fake)</option>
				{#each agents as a (a)}
					<option value={a}>{a}</option>
				{/each}
			</select>
		</label>

		<label class="block">
			<span class="text-sm font-medium">cwd <span class="text-gray-400">(optional)</span></span>
			<input name="cwd" placeholder="subdir/within/repo" class="mt-1 w-full rounded border p-2" />
		</label>

		<div class="grid grid-cols-2 gap-3">
			<label class="block">
				<span class="text-sm font-medium">branch <span class="text-gray-400">(optional)</span></span
				>
				<input name="branch" class="mt-1 w-full rounded border p-2" />
			</label>
			<label class="block">
				<span class="text-sm font-medium"
					>base_ref <span class="text-gray-400">(optional)</span></span
				>
				<input name="base_ref" class="mt-1 w-full rounded border p-2" />
			</label>
		</div>

		<label class="block">
			<span class="text-sm font-medium">prompt <span class="text-gray-400">(optional)</span></span>
			<textarea name="prompt" rows="4" class="mt-1 w-full rounded border p-2"></textarea>
		</label>

		<button
			type="submit"
			disabled={createSession.pending > 0}
			class="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
		>
			{createSession.pending > 0 ? 'Starting…' : 'Start session'}
		</button>
	</form>

	{#if createSession.result}
		{#if createSession.result.ok}
			<div class="mt-4 rounded border border-green-200 bg-green-50 p-3 text-sm">
				<p class="font-medium text-green-800">Session started</p>
				<p class="text-green-700">
					id: <code>{createSession.result.id}</code> · status: {createSession.result.status}
				</p>
				<a
					href="/sessions/{createSession.result.id}"
					class="mt-1 inline-block text-blue-600 hover:underline"
				>
					Open session →
				</a>
			</div>
		{:else}
			<p class="mt-4 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
				{createSession.result.error}
			</p>
		{/if}
	{/if}
</section>
