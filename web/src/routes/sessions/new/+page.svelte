<script lang="ts">
	import { onMount, tick } from 'svelte';

	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { PersistedState, watch } from 'runed';
	import { getHosts } from '$lib/remote-functions/hosts.remote';
	import { getRepoRefs } from '$lib/remote-functions/repos.remote';
	import { getModels, getConversations, getSettings } from '$lib/remote-functions/codex.remote';
	import { createSession, getSessions } from '$lib/remote-functions/sessions.remote';
	import CodexSettings from '$lib/components/codex-settings.svelte';

	const hosts = getHosts();
	const lastHost = new PersistedState('tethon:lastHost', '');
	const lastRepo = new PersistedState<Record<string, string>>('tethon:lastRepo', {});
	const draft = new PersistedState('tethon:new-session-draft', { prompt: '', repo: '', cwd: '' });

	let hostId = $state(page.url.searchParams.get('host') ?? lastHost.current);
	let repoId = $state('');
	let agent = $state('');
	let model = $state('');
	let effort = $state('');
	let permission = $state('default');
	let permissionProfile = $state('');
	let workspace = $state('existing');
	let rawPath = $state(false);
	let baseRef = $state('');
	let mode = $state('new');
	let threadId = $state('');
	let search = $state('');
	let cursor = $state('');
	let createRequestId = $state('');
	let restoredAttempt = $state(false);
	let submissionError = $state('');
	let createdID = $state('');
	let createAttempt: { fingerprint: string; requestID: string } | undefined;

	const onlineHosts = $derived(
		hosts.current?.ok ? hosts.current.hosts.filter((h) => h.online) : []
	);
	const hostError = $derived(
		hosts.error?.message ?? (hosts.current?.ok === false ? hosts.current.error : '')
	);
	const host = $derived(onlineHosts.find((h) => h.host_id === hostId));
	const repos = $derived(host?.repos ?? []);
	const agents = $derived(host?.agents ?? []);
	const rawRepo = $derived(rawPath || !repos.length);
	const native = $derived(agent === 'codex-cli');
	const resume = $derived(native && mode === 'resume');

	const models = $derived(hostId && agent ? getModels({ host_id: hostId, agent }) : undefined);
	const catalog = $derived(models?.current?.ok ? models.current.value : undefined);
	const modelError = $derived(
		models?.error?.message ?? (models?.current?.ok === false ? models.current.error : '')
	);
	const choices = $derived(catalog?.models ?? []);
	const modelDisabled = $derived(
		!choices.length ||
			Boolean(models?.loading) ||
			Boolean(model && !choices.some((choice) => choice.id === model))
	);
	const efforts = $derived(choices.find((m) => m.id === model)?.reasoning_efforts ?? []);
	const conversations = $derived(
		resume && hostId ? getConversations({ host_id: hostId, search, cursor }) : undefined
	);
	const conversationPage = $derived(
		conversations?.current?.ok ? conversations.current.value : undefined
	);
	const conversationError = $derived(
		conversations?.error?.message ??
			(conversations?.current?.ok === false ? conversations.current.error : '')
	);
	const refs = $derived(
		!rawRepo && hostId && repoId ? getRepoRefs({ host_id: hostId, repo_id: repoId }) : undefined
	);
	const refOptions = $derived(
		refs?.current?.ok ? [...refs.current.branches, ...refs.current.tags] : []
	);

	const settingsCWD = $derived(
		resume
			? conversationPage?.conversations?.find((c) => c.thread_id === threadId)?.cwd
			: `${(rawRepo ? draft.current.repo : repos.find((r) => r.id === repoId)?.path) || ''}${draft.current.cwd ? `/${draft.current.cwd}` : ''}`
	);

	const settings = $derived(
		native && hostId ? getSettings({ host_id: hostId, cwd: settingsCWD || undefined }) : undefined
	);
	const permissionProfiles = $derived(
		settings?.current?.ok
			? (settings.current.value.permission_profiles?.profiles ?? []).filter(
					(profile) => profile.allowed
				)
			: []
	);
	const hasOverrides = $derived(Boolean(model || effort || permissionProfile));
	const promptRequired = $derived(native && (!resume || hasOverrides));

	$effect(() => {
		if (!onlineHosts.some((h) => h.host_id === hostId) && onlineHosts[0])
			hostId = onlineHosts[0].host_id;
		if (host && !agents.includes(agent))
			agent = agents.includes('codex-cli') ? 'codex-cli' : (agents[0] ?? '');
		if (!repoId && repos.length) repoId = lastRepo.current[hostId] || repos[0].id;
	});

	watch(
		() => hostId,
		(id) => {
			lastHost.current = id;
			repoId = '';
			threadId = '';
			cursor = '';
		}
	);
	watch(
		() => [hostId, agent],
		() => {
			model = '';
			effort = '';
			permissionProfile = '';
			threadId = '';
		}
	);
	watch(
		() => model,
		() => {
			effort = '';
		}
	);
	watch(
		() => search,
		() => {
			cursor = '';
		}
	);
	watch(
		() => repoId,
		(id) => {
			baseRef = repos.find((r) => r.id === id)?.default_branch ?? '';
		}
	);

	createSession.enhance(async (form) => {
		submissionError = '';
		try {
			await form.submit().updates();
		} catch {
			submissionError =
				'The connection ended before acknowledgement. Retry the unchanged request with the same request key.';
			return;
		}
		if (!form.result?.ok) return;

		createdID = form.result.id;
		if (repoId) lastRepo.current = { ...lastRepo.current, [hostId]: repoId };
		draft.current.prompt = '';
		createRequestId = '';
		createAttempt = undefined;
		try {
			localStorage.removeItem(createAttemptKey());
		} catch {
			/* The acknowledged attempt is complete. */
		}

		try {
			await getSessions().refresh();
		} catch {
			/* The destination also refreshes session metadata. */
		}
		try {
			await goto(`/sessions/${createdID}`);
		} catch {
			submissionError = 'The session was created. Use the link below to open it.';
		}
	});

	const createAttemptKey = () => `tethon:create-attempt:${location.origin}`;

	// Capture runs before SvelteKit creates its submission FormData. An unchanged
	// retry must keep its UUID even when the first response was lost.
	const prepareCreate = (event: SubmitEvent & { currentTarget: HTMLFormElement }) => {
		const form = event.currentTarget;
		const input = form.elements.namedItem('request_id') as HTMLInputElement;
		if (!native) {
			input.value = '';
			createRequestId = '';
			return;
		}

		const entries = [...new FormData(form).entries()]
			.filter(([key]) => key !== 'request_id')
			.map(([key, value]) => [key, String(value).trim()])
			.filter(([, value]) => value)
			.sort(([left], [right]) => left.localeCompare(right));
		const fingerprint = JSON.stringify(entries);
		let attempt = createAttempt;
		try {
			attempt = JSON.parse(localStorage.getItem(createAttemptKey()) || 'null') ?? createAttempt;
		} catch {
			/* Keep the form usable without storage. */
		}
		if (attempt?.fingerprint !== fingerprint || !attempt.requestID)
			attempt = { fingerprint, requestID: crypto.randomUUID() };

		input.value = attempt.requestID;
		createRequestId = attempt.requestID;
		createAttempt = attempt;
		try {
			localStorage.setItem(createAttemptKey(), JSON.stringify(attempt));
		} catch {
			/* The current form still retains its UUID. */
		}
	};

	onMount(() => {
		void (async () => {
			try {
				const attempt = JSON.parse(localStorage.getItem(createAttemptKey()) || 'null');
				if (typeof attempt?.fingerprint !== 'string' || typeof attempt.requestID !== 'string')
					return;
				const entries = JSON.parse(attempt.fingerprint);
				if (
					!Array.isArray(entries) ||
					!entries.every(
						(entry) =>
							Array.isArray(entry) &&
							entry.length === 2 &&
							entry.every((part) => typeof part === 'string')
					)
				)
					return;

				const saved = Object.fromEntries(entries) as Record<string, string>;
				if (
					saved.agent !== 'codex-cli' ||
					(page.url.searchParams.has('host') &&
						page.url.searchParams.get('host') !== saved.host_id) ||
					(saved.prompt ?? '') !== draft.current.prompt.trim()
				)
					return;
				await hosts.refresh();
				await tick();
				if (!onlineHosts.some((host) => host.host_id === saved.host_id)) return;

				// Let dependent selectors reset before restoring the saved payload.
				hostId = saved.host_id;
				await tick();
				agent = saved.agent;
				await tick();
				repoId = saved.repo_id ?? '';
				rawPath = Boolean(saved.repo);
				draft.current.repo = saved.repo ?? '';
				draft.current.cwd = saved.cwd ?? '';
				workspace = saved.workspace_mode ?? 'existing';
				mode = saved.native_thread_id ? 'resume' : 'new';
				threadId = saved.native_thread_id ?? '';
				model = saved.requested_model ?? '';
				await tick();
				baseRef = saved.base_ref ?? '';
				effort = saved.reasoning_effort ?? '';
				permissionProfile = saved.codex_permission_profile ?? '';
				createAttempt = attempt;
				createRequestId = attempt.requestID;
				restoredAttempt = true;
			} catch {
				/* Invalid or disabled storage must not prevent a new request. */
			}
		})();
	});
</script>

<svelte:head>
	<title>New session · Tethon</title>
</svelte:head>

<section class="max-w-2xl">
	<h1 class="mb-5 text-xl font-semibold">Start a session</h1>
	{#if hostError}
		<p role="alert" class="mb-4 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			{hostError}
		</p>
	{/if}
	{#if hosts.ready && !onlineHosts.length}
		<p class="mb-4 text-sm text-gray-600">No host is online. Connect a host to start a session.</p>
	{/if}
	{#if restoredAttempt}
		<p
			role="status"
			class="mb-4 rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900"
		>
			An unconfirmed request was restored. An unchanged retry uses the same request key.
		</p>
	{/if}

	<form {...createSession} onsubmitcapture={prepareCreate} class="space-y-5">
		<input type="hidden" name="request_id" value={createRequestId} />

		{#each createSession.fields.request_id.issues() ?? [] as issue (issue.message)}
			<p role="alert" class="text-sm text-red-700">{issue.message}</p>
		{/each}

		<div class="grid gap-4 sm:grid-cols-2">
			<label class="block text-sm font-medium">
				<span>Host</span>
				<select name="host_id" bind:value={hostId} class="mt-1 w-full rounded border p-2">
					<option value="" disabled>Select a host</option>
					{#each onlineHosts as h (h.host_id)}
						<option value={h.host_id}>{h.name} </option>
					{/each}
				</select>
			</label>
			<label class="block text-sm font-medium">
				<span>Agent</span>
				<select name="agent" bind:value={agent} class="mt-1 w-full rounded border p-2">
					<option value="" disabled>Select an agent</option>
					{#each agents as a (a)}
						<option value={a}>{a} </option>
					{/each}
				</select>
			</label>
		</div>
		{#each createSession.fields.host_id.issues() ?? [] as issue (issue.message)}
			<p class="text-sm text-red-700">
				{issue.message}
			</p>
		{/each}

		{#if native}
			<fieldset class="rounded-lg border border-gray-200 p-4">
				<legend class="px-1 text-sm font-medium">Conversation</legend>
				<div class="flex flex-wrap gap-4 text-sm">
					<label class="flex items-center gap-2">
						<input type="radio" bind:group={mode} value="new" />
						<span>New conversation</span>
					</label>
					<label class="flex items-center gap-2">
						<input type="radio" bind:group={mode} value="resume" />
						<span>Find a conversation</span>
					</label>
				</div>
				{#if resume}
					<label class="mt-4 block text-sm">
						<span>Search conversations</span>
						<input
							type="search"
							bind:value={search}
							class="mt-1 w-full rounded border p-2"
							placeholder="Name or message"
						/>
					</label>
					{#if conversations?.loading}
						<p role="status" class="mt-3 text-sm text-gray-500">Loading conversations…</p>
					{/if}
					{#if conversationError}
						<p role="alert" class="mt-3 text-sm text-red-700">
							{conversationError}
						</p>
						<button
							type="button"
							onclick={() => conversations?.refresh()}
							class="mt-2 cursor-pointer text-sm text-blue-700">Try again</button
						>
					{/if}
					<div class="mt-3 max-h-72 space-y-2 overflow-y-auto">
						{#each conversationPage?.conversations ?? [] as c (c.thread_id)}
							<label
								class="flex cursor-pointer gap-3 rounded border p-3 {threadId === c.thread_id
									? 'border-blue-400 bg-blue-50'
									: 'border-gray-200'}"
							>
								<input type="radio" bind:group={threadId} value={c.thread_id} class="mt-1" />
								<span class="min-w-0">
									<span class="block truncate text-sm font-medium">
										{c.name || c.preview || 'Untitled conversation'}
									</span>
									<span class="mt-1 block truncate text-xs text-gray-500">
										{c.cwd}
									</span>
									<span class="mt-1 block text-xs text-gray-500">
										{c.status} · {new Date(c.updated_at * 1000).toLocaleDateString()}
									</span>
								</span>
							</label>
						{:else}
							{#if conversationPage}
								<p class="text-sm text-gray-500">No conversations found.</p>
							{/if}
						{/each}
					</div>
					{#if conversationPage?.next_cursor}
						<button
							type="button"
							class="mt-3 cursor-pointer text-sm text-blue-700"
							onclick={() => {
								cursor = conversationPage?.next_cursor ?? '';
							}}>Next conversations</button
						>
					{/if}
					{#if cursor}
						<button
							type="button"
							class="mt-3 ml-4 cursor-pointer text-sm text-blue-700"
							onclick={() => {
								cursor = '';
							}}>First page</button
						>
					{/if}
				{/if}
			</fieldset>
		{/if}
		<input type="hidden" name="native_thread_id" value={resume ? threadId : ''} />

		{#if native}
			<CodexSettings {hostId} cwd={settingsCWD} />
		{/if}

		{#if !resume}
			{#if rawRepo}
				<label class="block text-sm font-medium">
					<span>Working repository</span>
					<input
						name="repo"
						bind:value={draft.current.repo}
						placeholder="/absolute/path/to/repository"
						class="mt-1 w-full rounded border p-2"
					/>
				</label>
			{:else}
				<label class="block text-sm font-medium">
					<span>Repository</span>
					<select name="repo_id" bind:value={repoId} class="mt-1 w-full rounded border p-2">
						<option value="" disabled>Select a repository</option>
						{#each repos as r (r.id)}
							<option value={r.id}>{r.name} </option>
						{/each}
					</select>
				</label>
				<label class="flex items-center gap-2 text-sm text-gray-600">
					<input type="checkbox" bind:checked={rawPath} />Enter a repository path</label
				>
			{/if}
			<label class="block text-sm font-medium">
				<span>Subfolder <span class="font-normal text-gray-500">(optional)</span> </span>
				<input
					name="cwd"
					bind:value={draft.current.cwd}
					placeholder="Folder within the repository"
					class="mt-1 w-full rounded border p-2"
				/>
			</label>
			{#if native}
				<label class="block text-sm font-medium">
					<span>Working folder</span>
					<select
						name="workspace_mode"
						bind:value={workspace}
						class="mt-1 w-full rounded border p-2"
					>
						<option value="existing">Use the existing folder</option>
						<option value="isolated">Create an isolated Git worktree</option>
					</select>
				</label>
			{/if}
			{#if !native || workspace === 'isolated'}
				<div class="grid gap-3 sm:grid-cols-2">
					<label class="block text-sm font-medium">
						<span>Branch <span class="font-normal text-gray-500">(optional)</span> </span>
						<input name="branch" class="mt-1 w-full rounded border p-2" />
					</label>
					<label class="block text-sm font-medium">
						<span>Base branch or ref</span>
						<input
							name="base_ref"
							list="base-refs"
							bind:value={baseRef}
							class="mt-1 w-full rounded border p-2"
						/>
						<datalist id="base-refs">
							{#each refOptions as ref (ref)}
								<option value={ref}></option>
							{/each}
						</datalist>
					</label>
				</div>
			{/if}
		{/if}
		{#each createSession.fields.repo_id.issues() ?? [] as issue (issue.message)}
			<p class="text-sm text-red-700">
				{issue.message}
			</p>
		{/each}
		<label class="block text-sm font-medium">
			<span>Model</span>
			<select
				name="requested_model"
				bind:value={model}
				disabled={modelDisabled}
				class="mt-1 w-full rounded border p-2 disabled:bg-gray-50"
			>
				<option value="">Agent default</option>
				{#each choices as choice (choice.id)}
					<option value={choice.id}>{choice.label}{choice.is_default ? ' (default)' : ''} </option>
				{/each}
			</select>
		</label>
		{#if model && modelDisabled}
			<input type="hidden" name="requested_model" value={model} />
			<p class="text-sm text-gray-600">Saved request model: {model}</p>
			<button
				type="button"
				class="cursor-pointer text-sm text-blue-700"
				onclick={() => {
					model = '';
				}}>Use agent default</button
			>
		{/if}
		{#if native && effort && !efforts.length}
			<input type="hidden" name="reasoning_effort" value={effort} />
		{/if}
		{#if native && permissionProfile && !permissionProfiles.length}
			<input type="hidden" name="codex_permission_profile" value={permissionProfile} />
		{/if}

		{#if models?.loading}
			<p role="status" class="text-sm text-gray-500">Loading models…</p>
		{:else if modelError}
			<p role="alert" class="text-sm text-amber-800">
				{modelError} You can use the agent default.
			</p>
			<button
				type="button"
				class="cursor-pointer text-sm text-blue-700"
				onclick={() => models?.refresh()}>Retry models</button
			>
		{:else if catalog && !choices.length}
			<p class="text-sm text-gray-500">
				{catalog.message || 'Model choices are not available. The agent default will be used.'}
			</p>
		{/if}
		{#if native && efforts.length}
			<label class="block text-sm font-medium">
				<span>Reasoning effort</span>
				<select name="reasoning_effort" bind:value={effort} class="mt-1 w-full rounded border p-2">
					<option value="">Model default</option>
					{#each efforts as choice (choice.id)}
						<option value={choice.id}>
							{choice.id}{choice.description ? ` — ${choice.description}` : ''}
						</option>
					{/each}
				</select>
			</label>
		{/if}
		{#if native && permissionProfiles.length}
			<label class="block text-sm font-medium">
				<span>Permission profile</span>
				<select
					name="codex_permission_profile"
					bind:value={permissionProfile}
					class="mt-1 w-full rounded border p-2"
				>
					<option value="">Native default</option>
					{#each permissionProfiles as profile (profile.id)}
						<option value={profile.id}
							>{profile.id}{profile.description ? ` — ${profile.description}` : ''}</option
						>
					{/each}
				</select>
			</label>
		{/if}
		{#if resume && hasOverrides}
			<p class="text-sm text-gray-600">
				Enter a message to apply the selected model, effort, or permission profile on the next turn.
			</p>
		{/if}

		{#if agent === 'claude-cli'}
			<label class="block text-sm font-medium">
				<span>Permission mode</span>
				<select
					name="permission_mode"
					bind:value={permission}
					class="mt-1 w-full rounded border p-2"
				>
					<option value="default">Default</option>
					<option value="plan">Plan</option>
					<option value="acceptEdits">Accept edits</option>
					<option value="bypassPermissions">Bypass permissions</option>
				</select>
			</label>
		{/if}
		<label class="block text-sm font-medium">
			<span>
				Message
				{#if !promptRequired}
					<span class="font-normal text-gray-500">(optional)</span>
				{/if}
			</span>
			<textarea
				name="prompt"
				required={promptRequired}
				bind:value={draft.current.prompt}
				rows="4"
				class="mt-1 w-full rounded border p-2"
				placeholder={resume ? 'Continue this conversation…' : 'What should the agent do?'}
			></textarea>
		</label>
		{#each createSession.fields.prompt.issues() ?? [] as issue (issue.message)}
			<p role="alert" class="text-sm text-red-700">{issue.message}</p>
		{/each}

		<button
			type="submit"
			disabled={Boolean(createdID) || createSession.pending > 0 || !host || (resume && !threadId)}
			class="cursor-pointer rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:cursor-default disabled:opacity-50"
		>
			{createSession.pending > 0 ? 'Opening…' : resume ? 'Resume conversation' : 'Start session'}
		</button>
	</form>
	{#if submissionError}
		<p
			role="alert"
			class="mt-4 rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900"
		>
			{submissionError}
		</p>
	{/if}
	{#if createdID}
		<a href="/sessions/{createdID}" class="mt-3 inline-block text-sm text-blue-700 underline"
			>Open the created session</a
		>
	{/if}
	{#if createSession.result && !createSession.result.ok}
		<p role="alert" class="mt-4 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			{createSession.result.error}
		</p>
	{/if}
</section>
