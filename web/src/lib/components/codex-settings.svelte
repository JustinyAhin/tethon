<script lang="ts">
	import { getSettings } from '$lib/remote-functions/codex.remote';
	import { label } from '$lib/codex';

	let { hostId, cwd }: { hostId: string; cwd?: string } = $props();

	let open = $state(false);

	const query = $derived(
		open && hostId ? getSettings({ host_id: hostId, cwd: cwd || undefined }) : undefined
	);
	const settings = $derived(query?.current?.ok ? query.current.value : undefined);
	const error = $derived(
		query?.error?.message ?? (query?.current?.ok === false ? query.current.error : '')
	);
</script>

<details
	class="my-4 rounded-lg border border-gray-200 p-3"
	ontoggle={(event) => {
		open = event.currentTarget.open;
	}}
>
	<summary class="cursor-pointer text-sm font-medium">Codex account and settings</summary>

	{#if query?.loading}
		<p role="status" class="mt-3 text-sm text-gray-500">Reading native settings…</p>
	{/if}

	{#if error}
		<p role="alert" class="mt-3 text-sm text-red-700">{error}</p>
		<button
			type="button"
			class="mt-2 cursor-pointer text-sm text-blue-700"
			onclick={() => query?.refresh()}>Retry settings</button
		>
	{/if}

	{#if settings}
		<div class="mt-3 max-h-[50vh] space-y-4 overflow-y-auto pr-1 text-sm">
			<div>
				<h3 class="font-medium">Account</h3>
				<p class="mt-1 text-gray-600">
					{label(settings.account.status)}{settings.account.type
						? ` · ${label(settings.account.type)}`
						: ''}{settings.account.plan_type ? ` · ${settings.account.plan_type}` : ''}
				</p>
				{#if settings.account.login_command}
					<p class="mt-2 text-xs text-gray-500">
						Sign in on the host with <code>{settings.account.login_command} </code>.
					</p>
				{/if}
			</div>

			<div>
				<h3 class="font-medium">Native defaults</h3>
				<p class="mt-1 text-xs break-all text-gray-500">{settings.cwd}</p>
				<dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
					{#each Object.entries(settings.defaults) as [key, value] (key)}
						{#if value}
							<dt class="text-gray-500">{label(key)}</dt>
							<dd class="break-words">{value}</dd>
						{/if}
					{/each}
				</dl>
				{#if settings.config_status !== 'ready'}
					<p class="mt-2 text-xs text-gray-500">Configuration: {label(settings.config_status)}</p>
				{/if}
			</div>

			{#if settings.managed.configured}
				<div>
					<h3 class="font-medium">Managed settings</h3>
					{#if settings.managed.model_provider}
						<p class="mt-1">
							Provider: {settings.managed.model_provider}
						</p>
					{/if}
					{#each [['Approval policies', settings.managed.allowed_approval_policies], ['Sandbox modes', settings.managed.allowed_sandbox_modes], ['Web search modes', settings.managed.allowed_web_search_modes]] as [name, allowed] (String(name))}
						{#if Array.isArray(allowed)}
							<p class="mt-1 text-xs text-gray-600">
								{name}: {allowed.length ? allowed.join(', ') : 'No override is permitted'}
							</p>
						{/if}
					{/each}
				</div>
			{/if}

			<div>
				<h3 class="font-medium">Rate limits</h3>
				{#if settings.rate_limits.ordinary_usage_allowed === false}
					<p class="mt-1 text-amber-800">Ordinary usage is not available for this account.</p>
				{/if}
				{#each settings.rate_limits.buckets ?? [] as bucket, index (bucket.id || String(index))}
					<div class="mt-2">
						<p class="text-xs">{bucket.name || bucket.id || 'Usage window'}</p>
						{#each [bucket.primary, bucket.secondary] as window, windowIndex (windowIndex)}
							{#if window}
								<div class="mt-1 flex flex-wrap items-center gap-2">
									<meter
										min="0"
										max="100"
										value={window.used_percent}
										class="min-w-20 flex-1"
										aria-label="Usage percentage"
									></meter>
									<span class="text-xs text-gray-500">
										{Math.round(window.used_percent)}% used{window.resets_at
											? ` · resets ${new Date(window.resets_at * 1000).toLocaleString()}`
											: ''}
									</span>
								</div>
							{/if}
						{/each}
					</div>
				{:else}
					<p class="mt-1 text-xs text-gray-500">
						{label(settings.rate_limits.status)} · No usage windows reported.
					</p>
				{/each}
			</div>

			<div>
				<h3 class="font-medium">Skills</h3>
				<ul class="mt-1 space-y-2">
					{#each settings.skills.skills ?? [] as skill (skill.name)}
						<li>
							<span>{skill.display_name || skill.name} </span>
							<span class="ml-2 text-xs text-gray-500">
								{skill.enabled ? 'Enabled' : 'Disabled'} · {skill.scope}
							</span>
							{#if skill.description}
								<p class="text-xs text-gray-500">{skill.description}</p>
							{/if}
						</li>
					{:else}
						<li class="text-xs text-gray-500">
							{label(settings.skills.status)} · No skills reported.
						</li>
					{/each}
				</ul>
			</div>

			<div>
				<h3 class="font-medium">
					MCP servers <span class="text-xs font-normal text-gray-500">(host scope)</span>
				</h3>
				{#each settings.mcp.servers ?? [] as server (server.name)}
					<details class="mt-2">
						<summary class="cursor-pointer">
							{server.name} · {label(server.runtime_status || server.auth_status)}
						</summary>
						<p class="mt-1 text-xs text-gray-500">
							{(server.tools ?? []).join(', ') || `Tools: ${label(server.tools_status)}`}
						</p>
					</details>
				{:else}
					<p class="mt-1 text-xs text-gray-500">
						{label(settings.mcp.status)} · No servers reported.
					</p>
				{/each}
			</div>

			{#if settings.collaboration.modes?.length}
				<div>
					<h3 class="font-medium">Collaboration modes</h3>
					<p class="mt-1 text-xs text-gray-600">
						{settings.collaboration.modes.map((mode) => mode.name).join(', ')}
					</p>
				</div>
			{/if}

			{#each settings.warnings ?? [] as warning (warning)}
				<p class="text-xs text-amber-800">
					{warning}
				</p>
			{/each}
		</div>
	{/if}
</details>
