<script lang="ts">
	import { onMount } from 'svelte';

	import type { CodexRequestPayload } from '@tethon/hub-sdk/protocol';

	import { object, list, text, label, type NativeObject } from '$lib/codex';

	let {
		request,
		disabled = false,
		onrespond
	}: {
		request: CodexRequestPayload;
		disabled?: boolean;
		onrespond: (result: unknown) => Promise<void>;
	} = $props();

	let sending = $state(false);
	let error = $state('');
	let answers = $state<Record<string, string>>({});
	let fields = $state<Record<string, string | boolean | number | string[]>>({});
	let grantNetwork = $state(false);
	let grantFiles = $state(false);
	let grantScope = $state('turn');

	const params = $derived(object(request.params));
	const questions = $derived(list(params.questions).map(object));
	const permissions = $derived(object(params.permissions));
	const schema = $derived(object(params.requestedSchema));
	const properties = $derived(
		Object.entries(object(schema.properties)).map(([key, value]) => ({
			key,
			schema: object(value)
		}))
	);
	const safeURL = $derived.by(() => {
		try {
			const url = new URL(text(params.url));
			return ['https:', 'http:'].includes(url.protocol) ? url.href : '';
		} catch {
			return '';
		}
	});
	const schemaSupported = $derived(
		schema.type === 'object' &&
			properties.every(
				({ schema: field }) =>
					['string', 'number', 'integer', 'boolean'].includes(text(field.type)) ||
					(field.type === 'array' &&
						object(field.items).type === 'string' &&
						Array.isArray(object(field.items).enum))
			)
	);
	const unavailable = $derived(disabled || request.unsupported || request.truncated);
	const commandApproval = $derived(request.method === 'item/commandExecution/requestApproval');
	const fileApproval = $derived(request.method === 'item/fileChange/requestApproval');
	const permissionApproval = $derived(request.method === 'item/permissions/requestApproval');
	const decisions = $derived(
		Array.isArray(params.availableDecisions)
			? params.availableDecisions
			: ['accept', 'acceptForSession', 'decline', 'cancel']
	);

	const decisionLabel = (decision: unknown) =>
		({
			accept: 'Allow once',
			acceptForSession: 'Allow for this session',
			decline: 'Decline',
			cancel: 'Cancel request'
		})[text(decision)] ||
		(object(decision).acceptWithExecpolicyAmendment
			? 'Allow this command pattern'
			: object(decision).applyNetworkPolicyAmendment
				? 'Apply network rule'
				: 'Unsupported decision');
	const knownDecision = (decision: unknown) =>
		['accept', 'acceptForSession', 'decline', 'cancel'].includes(text(decision)) ||
		Boolean(
			object(decision).acceptWithExecpolicyAmendment || object(decision).applyNetworkPolicyAmendment
		);

	const submit = async (result: unknown) => {
		if (unavailable || sending) return;
		sending = true;
		error = '';
		try {
			await onrespond(result);
		} catch (err) {
			error = err instanceof Error ? err.message : 'The request could not be answered.';
		} finally {
			sending = false;
		}
	};

	const answerQuestions = () => {
		if (questions.some((q) => !text(q.id) || !answers[text(q.id)]?.trim())) {
			error = 'Answer each question before sending.';
			return;
		}
		void submit({
			answers: Object.fromEntries(
				questions.map((q) => [text(q.id), { answers: [answers[text(q.id)]] }])
			)
		});
	};

	const fieldOptions = (field: NativeObject) =>
		Array.isArray(field.enum)
			? list(field.enum).map((value) => ({ value: text(value), title: text(value) }))
			: list(field.oneOf).map((choice) => ({
					value: text(object(choice).const),
					title: text(object(choice).title) || text(object(choice).const)
				}));

	const permissionPath = (entry: unknown) => {
		const path = object(object(entry).path);
		if (path.type === 'path') return text(path.path);
		if (path.type === 'glob_pattern') return text(path.pattern);
		if (path.type === 'special') {
			const special = object(path.value);
			return [label(special.kind), text(special.path), text(special.subpath)]
				.filter(Boolean)
				.join(' / ');
		}
		return text(object(entry).path) || 'Native file rule';
	};

	const submitMCP = () => {
		const content: Record<string, unknown> = {};
		for (const { key, schema: field } of properties) {
			const value = fields[key] ?? field.default;
			if (list(schema.required).includes(key) && (value === undefined || value === '')) {
				error = `Enter ${text(field.title) || key}.`;
				return;
			}
			if (value === undefined || value === '') continue;
			if (field.type === 'number' || field.type === 'integer') {
				const number = Number(value);
				if (
					!Number.isFinite(number) ||
					(field.type === 'integer' && !Number.isInteger(number)) ||
					(typeof field.minimum === 'number' && number < field.minimum) ||
					(typeof field.maximum === 'number' && number > field.maximum)
				) {
					error = `Enter a valid number for ${key}.`;
					return;
				}
				content[key] = number;
			} else content[key] = value;
		}
		void submit({ action: 'accept', content, _meta: params._meta ?? null });
	};

	onMount(() => {
		fields = Object.fromEntries(
			properties.map(({ key, schema: field }) => [
				key,
				field.default ?? (field.type === 'boolean' ? false : field.type === 'array' ? [] : '')
			])
		) as typeof fields;
	});
</script>

<section class="rounded-lg border border-amber-300 bg-amber-50 p-4" aria-label="Codex request">
	<h3 class="text-sm font-semibold text-amber-900">
		{request.kind === 'user_input'
			? 'Codex has a question'
			: request.kind === 'mcp'
				? `${text(params.serverName) || 'MCP server'} needs input`
				: permissionApproval
					? 'Permission request'
					: fileApproval
						? 'File change approval'
						: 'Command approval'}
	</h3>
	{#if params.reason || params.message}
		<p class="mt-2 text-sm whitespace-pre-wrap text-amber-900">
			{text(params.reason) || text(params.message)}
		</p>
	{/if}
	{#if params.command}
		<code class="mt-3 block overflow-x-auto rounded bg-white p-3 text-xs whitespace-pre-wrap"
			>{text(params.command)}</code
		>
	{/if}
	{#if params.cwd}
		<p class="mt-2 text-xs break-all text-amber-800">{text(params.cwd)}</p>
	{/if}
	{#if params.networkApprovalContext}
		<p class="mt-2 text-sm">
			Network: {text(object(params.networkApprovalContext).host)} · {text(
				object(params.networkApprovalContext).protocol
			)}
		</p>
	{/if}
	{#if unavailable}
		<p class="mt-3 text-sm text-amber-900">
			{request.truncated
				? 'This request exceeds the display limit.'
				: 'This request is no longer available on this connection.'} Check the current request in Codex.
		</p>
	{:else if commandApproval || fileApproval}
		{#if params.proposedExecpolicyAmendment}
			<p class="mt-2 text-xs break-all">
				Proposed command pattern: {list(params.proposedExecpolicyAmendment).map(text).join(' ')}
			</p>
		{/if}
		<div class="mt-4 flex flex-wrap gap-2">
			{#each decisions as decision, index (index)}
				<button
					type="button"
					disabled={sending || !knownDecision(decision)}
					onclick={() => submit({ decision })}
					class="cursor-pointer rounded border border-amber-300 bg-white px-3 py-2 text-sm text-amber-900 disabled:opacity-50"
				>
					{decisionLabel(decision)}
				</button>
			{/each}
		</div>
	{:else if permissionApproval}
		<div class="mt-3 space-y-3 text-sm">
			{#if permissions.network}
				<label class="flex items-center gap-2">
					<input type="checkbox" bind:checked={grantNetwork} disabled={sending} />Allow the
					requested network access</label
				>
			{/if}
			{#if permissions.fileSystem}
				<label class="flex items-center gap-2">
					<input type="checkbox" bind:checked={grantFiles} disabled={sending} />Allow the requested
					file access</label
				>
				<ul class="ml-5 list-disc text-xs break-all">
					{#each list(object(permissions.fileSystem).read) as path, index (index)}
						<li>
							Read: {text(path)}
						</li>
					{/each}

					{#each list(object(permissions.fileSystem).write) as path, index (index)}
						<li>
							Write: {text(path)}
						</li>
					{/each}

					{#each list(object(permissions.fileSystem).entries) as entry, index (index)}
						<li>
							{label(object(entry).access)}: {permissionPath(entry)}
						</li>
					{/each}
				</ul>
			{/if}
			<label class="block">
				Grant duration<select bind:value={grantScope} class="ml-3 rounded border bg-white p-2">
					<option value="turn">This turn</option>
					<option value="session">This session</option>
				</select>
			</label>
			<button
				disabled={sending}
				onclick={() =>
					submit({
						permissions: {
							...(grantNetwork ? { network: permissions.network } : {}),
							...(grantFiles ? { fileSystem: permissions.fileSystem } : {})
						},
						scope: grantScope
					})}
				class="cursor-pointer rounded border border-amber-300 bg-white px-3 py-2"
			>
				Send selected permissions</button
			>
		</div>
	{:else if request.kind === 'user_input' && questions.length}
		<form
			class="mt-4 space-y-4"
			onsubmit={(e) => {
				e.preventDefault();
				answerQuestions();
			}}
		>
			{#each questions as question (text(question.id))}
				<fieldset disabled={sending}>
					<legend class="text-sm font-medium">{text(question.header)} </legend>
					<p class="mt-1 text-sm">{text(question.question)}</p>
					{#each list(question.options).map(object) as option (text(option.label))}
						<label class="mt-2 flex cursor-pointer items-start gap-2 text-sm">
							<input
								type="radio"
								name={`${request.request_key}-${text(question.id)}`}
								bind:group={answers[text(question.id)]}
								value={text(option.label)}
								class="mt-1"
							/>
							<span>
								{text(option.label)}{#if option.description}
									<span class="block text-xs text-gray-600">{text(option.description)} </span>
								{/if}
							</span>
						</label>
					{/each}
					{#if question.isOther || !list(question.options).length}
						<label class="mt-2 block text-sm">
							{question.isOther ? 'Your answer' : 'Answer'}
							<input
								type={question.isSecret ? 'password' : 'text'}
								bind:value={answers[text(question.id)]}
								class="mt-1 w-full rounded border bg-white p-2"
								autocomplete="off"
							/>
						</label>
					{/if}
				</fieldset>
			{/each}
			<button
				disabled={sending}
				class="cursor-pointer rounded bg-amber-800 px-3 py-2 text-sm text-white disabled:opacity-50"
			>
				{sending ? 'Sending…' : 'Send answers'}
			</button>
		</form>
	{:else if request.kind === 'mcp'}
		{#if params.mode === 'url' && safeURL}
			<a
				href={safeURL}
				target="_blank"
				rel="noopener noreferrer"
				class="mt-3 inline-block text-sm text-blue-700 underline">Open the requested page ↗</a
			>
			<p class="mt-2 text-xs text-gray-600">Complete the task on that page, then confirm below.</p>
			<button
				disabled={sending}
				onclick={() => submit({ action: 'accept', content: null, _meta: params._meta ?? null })}
				class="mt-3 cursor-pointer rounded border bg-white px-3 py-2 text-sm"
			>
				I completed this step</button
			>
		{:else if schemaSupported}
			<form
				class="mt-3 space-y-3"
				onsubmit={(e) => {
					e.preventDefault();
					submitMCP();
				}}
			>
				{#each properties as { key, schema: field } (key)}
					<label class="block text-sm">
						{text(field.title) || key}{list(schema.required).includes(key) ? ' *' : ''}
						{#if field.type === 'boolean'}
							<input type="checkbox" bind:checked={fields[key] as boolean} class="ml-2" />
						{:else if fieldOptions(field).length}
							<select bind:value={fields[key]} class="mt-1 w-full rounded border bg-white p-2">
								<option value="">Select an option</option>
								{#each fieldOptions(field) as option (option.value)}
									<option value={option.value}>{option.title} </option>
								{/each}
							</select>
						{:else if field.type === 'array'}
							<select
								multiple
								bind:value={fields[key] as string[]}
								class="mt-1 w-full rounded border bg-white p-2"
							>
								{#each list(object(field.items).enum) as option, index (index)}
									<option value={text(option)}>{text(option)} </option>
								{/each}
							</select>
						{:else}
							<input
								type={field.type === 'string'
									? field.format === 'email'
										? 'email'
										: 'text'
									: 'number'}
								step={field.type === 'integer' ? '1' : 'any'}
								bind:value={fields[key] as string}
								class="mt-1 w-full rounded border bg-white p-2"
							/>
						{/if}
						{#if field.description}
							<span class="mt-1 block text-xs text-gray-600">{text(field.description)} </span>
						{/if}
					</label>
				{/each}
				<button
					disabled={sending}
					class="cursor-pointer rounded bg-amber-800 px-3 py-2 text-sm text-white">Send form</button
				>
			</form>
		{:else}
			<p class="mt-3 text-sm text-amber-900">
				This form needs controls that this client cannot display. Open it in Codex.
			</p>
		{/if}
		<div class="mt-3 flex gap-2">
			<button
				disabled={sending}
				onclick={() => submit({ action: 'decline', content: null, _meta: params._meta ?? null })}
				class="cursor-pointer rounded border bg-white px-3 py-2 text-sm">Decline</button
			>
			<button
				disabled={sending}
				onclick={() => submit({ action: 'cancel', content: null, _meta: params._meta ?? null })}
				class="cursor-pointer rounded border bg-white px-3 py-2 text-sm">Cancel request</button
			>
		</div>
	{:else}
		<p class="mt-3 text-sm">This native request is not supported here. Open it in Codex.</p>
	{/if}
	{#if error}
		<p role="alert" class="mt-3 text-sm text-red-700">{error}</p>
	{/if}
</section>
