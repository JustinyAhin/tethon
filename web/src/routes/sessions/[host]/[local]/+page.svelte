<script lang="ts">
	import { onMount } from 'svelte';
	import { SvelteSet } from 'svelte/reactivity';
	import { PersistedState } from 'runed';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import MarkdownOutput from '$lib/components/markdown-output.svelte';
	import {
		sendInput,
		resolveApproval,
		cancelSession,
		deleteSession
	} from '$lib/remote-functions/sessions.remote';
	import type {
		SessionEvent,
		SessionStartedPayload,
		OutputPayload,
		InputReceivedPayload,
		ApprovalPayload,
		ApprovalResolvedPayload,
		ToolResultPayload,
		UsagePayload,
		DonePayload,
		ErrorPayload
	} from '$lib/types';

	type ApprovalAction = 'approve' | 'deny' | 'reply';

	const id = `${page.params.host}/${page.params.local}`;
	const streamUrl = `/sessions/${page.params.host}/${page.params.local}/stream`;
	const persisted = new PersistedState<SessionEvent[]>(`tethon:events:${id}`, []);

	let events = $state<SessionEvent[]>(persisted.current);
	const seen = new SvelteSet<number>(events.map((e) => e.seq));

	let connected = $state(false);
	let done = $state(events.some((e) => e.type === 'done' || e.type === 'error'));
	let streamError = $state('');
	let logEl = $state<HTMLElement>();
	let replyOpen = $state<Record<string, boolean>>({});
	let replyText = $state<Record<string, string>>({});
	let approvalError = $state<Record<string, string>>({});
	let approvalSubmitting = $state<Record<string, boolean>>({});
	let copyStatus = $state('');
	let copyError = $state('');

	const status = $derived(
		streamError ? 'error' : done ? 'finished' : connected ? 'live' : 'connecting…'
	);
	const resolvedApprovals = $derived.by(() => {
		const resolved: Record<string, ApprovalResolvedPayload> = {};
		for (const event of events) {
			if (event.type !== 'approval_resolved') continue;
			const payload = event.payload as ApprovalResolvedPayload;
			resolved[payload.request_id] = payload;
		}
		return resolved;
	});

	const parseEvent = (frame: string): SessionEvent | null => {
		for (const line of frame.split('\n')) {
			if (line.startsWith('data:')) {
				try {
					return JSON.parse(line.slice(5).trimStart()) as SessionEvent;
				} catch {
					return null;
				}
			}
		}
		return null;
	};

	// Stream the SSE relay (same-origin +server.ts) from the last seen cursor.
	// Reading the body in the browser is fine; only the Worker can't re-emit it.
	let controller: AbortController | null = null;

	const consume = async () => {
		if (done) return;
		controller?.abort();
		const ac = new AbortController();
		controller = ac;
		streamError = '';
		const from = events.reduce((max, e) => Math.max(max, e.seq), 0);
		try {
			const res = await fetch(`${streamUrl}?from=${from}`, {
				headers: { accept: 'text/event-stream' },
				signal: ac.signal
			});
			if (!res.ok || !res.body) {
				streamError = `Stream error ${res.status}`;
				return;
			}
			connected = true;
			const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
			let buffer = '';
			for (;;) {
				const { done: chunkDone, value } = await reader.read();
				if (chunkDone) break;
				buffer += value;
				let added = false;
				let sep: number;
				while ((sep = buffer.indexOf('\n\n')) !== -1) {
					const ev = parseEvent(buffer.slice(0, sep));
					buffer = buffer.slice(sep + 2);
					if (!ev || seen.has(ev.seq)) continue;
					seen.add(ev.seq);
					events.push(ev);
					added = true;
					if (ev.type === 'done' || ev.type === 'error') done = true;
				}
				// Persist outside any reactive effect to avoid a self-invalidating
				// update loop (writing PersistedState inside $effect re-triggers it).
				if (added) persisted.current = events.slice();
			}
		} catch (err) {
			if (ac.signal.aborted) return;
			streamError = err instanceof Error ? err.message : 'disconnected';
		} finally {
			if (controller === ac) {
				connected = false;
				controller = null;
			}
		}
	};

	const reconnect = () => {
		void consume();
	};

	onMount(() => {
		let stopped = false;
		(async () => {
			// Reconnect from cursor until the session is done or the page unmounts.
			while (!stopped && !done) {
				await consume();
				if (stopped || done) break;
				await new Promise((resolve) => setTimeout(resolve, 1000));
			}
		})();
		return () => {
			stopped = true;
			controller?.abort();
		};
	});

	$effect(() => {
		if (events.length && logEl) logEl.scrollTo({ top: logEl.scrollHeight });
	});

	let text = $state('');
	let sendError = $state('');
	let cancelError = $state('');
	let deleteError = $state('');

	const cancel = async () => {
		if (done || cancelSession.pending > 0) return;
		const res = await cancelSession({ id });
		if (res.ok) {
			cancelError = '';
		} else {
			cancelError = res.error;
		}
	};

	const deleteCurrentSession = async () => {
		if (!done || deleteSession.pending > 0) return;
		if (!window.confirm('Permanently delete this session? This cannot be undone.')) return;
		const res = await deleteSession({ id });
		if (res.ok) {
			deleteError = '';
			persisted.disconnect();
			await goto('/sessions', { invalidateAll: true });
		} else {
			deleteError = res.error;
		}
	};

	const send = async () => {
		if (!text.trim() || sendInput.pending > 0) return;
		const res = await sendInput({ id, text });
		if (res.ok) {
			text = '';
			sendError = '';
		} else {
			sendError = res.error;
		}
	};

	const formatPayload = (payload: unknown): string => {
		if (payload == null) return '';
		if (typeof payload === 'string') return payload;
		return JSON.stringify(payload, null, 2);
	};

	const eventTranscript = (event: SessionEvent): string => {
		switch (event.type) {
			case 'session_started': {
				const p = event.payload as SessionStartedPayload;
				const lines = [
					`## Session started`,
					`- seq: ${event.seq}`,
					`- repo: ${p.repo ?? ''}`,
					`- cwd: ${p.cwd ?? ''}`,
					`- branch: ${p.branch ?? ''}`,
					`- agent: ${p.agent ?? ''}`
				];
				if (p.permission_mode) lines.push(`- permission_mode: ${p.permission_mode}`);
				if (p.prompt) lines.push('', `Prompt:`, p.prompt);
				return lines.join('\n');
			}
			case 'input_received':
				return `## User\n\n${(event.payload as InputReceivedPayload).text}`;
			case 'output':
				return `## Agent\n\n${(event.payload as OutputPayload).text}`;
			case 'awaiting_approval': {
				const p = event.payload as ApprovalPayload;
				const lines = [
					`## Awaiting approval`,
					`- request_id: ${p.request_id}`,
					`- command: ${p.command || p.tool_name || 'tool request'}`
				];
				if (p.reason) lines.push(`- reason: ${p.reason}`);
				if (p.agent) lines.push(`- agent: ${p.agent}`);
				if (p.tool_name) lines.push(`- tool: ${p.tool_name}`);
				if (p.tool_input)
					lines.push('', 'Tool input:', '```json', formatPayload(p.tool_input), '```');
				return lines.join('\n');
			}
			case 'approval_resolved': {
				const p = event.payload as ApprovalResolvedPayload;
				return `## Approval resolved\n\n- request_id: ${p.request_id}\n- action: ${p.action}${p.text ? `\n\n${p.text}` : ''}`;
			}
			case 'tool_result': {
				const p = event.payload as ToolResultPayload;
				const lines = [`## Tool result`, `- tool: ${p.tool_name}`, `- status: ${p.status}`];
				if (p.tool_use_id) lines.push(`- tool_use_id: ${p.tool_use_id}`);
				if (p.approval_request_id) lines.push(`- approval_request_id: ${p.approval_request_id}`);
				if (p.duration_ms != null) lines.push(`- duration_ms: ${p.duration_ms}`);
				if (p.redacted) lines.push(`- redacted: true`);
				if (p.truncated) lines.push(`- truncated: true`, `- max_bytes: ${p.max_bytes ?? ''}`);
				if (p.tool_input)
					lines.push('', 'Tool input:', '```json', formatPayload(p.tool_input), '```');
				if (p.response) lines.push('', 'Response:', '```json', formatPayload(p.response), '```');
				if (p.error) lines.push('', 'Error:', p.error);
				return lines.join('\n');
			}
			case 'usage': {
				const p = event.payload as UsagePayload;
				return `## Usage\n\n- model: ${p.model ?? ''}\n- total_billable: ${p.tokens?.total_billable ?? 0}${p.cost ? `\n- total_usd: ${p.cost.total_usd}` : ''}`;
			}
			case 'done':
				return `## Done\n\n${(event.payload as DonePayload).reason}`;
			case 'error':
				return `## Error\n\n${(event.payload as ErrorPayload).message}`;
			default:
				return `## ${event.type}\n\n\`\`\`json\n${formatPayload(event.payload)}\n\`\`\``;
		}
	};

	const transcriptText = () =>
		[`# Tethon session transcript`, ``, `Session: ${id}`, `Events: ${events.length}`, ``]
			.concat(events.map(eventTranscript))
			.join('\n\n');

	const debugJSON = () =>
		JSON.stringify(
			{
				session_id: id,
				exported_at: new Date().toISOString(),
				event_count: events.length,
				events
			},
			null,
			2
		);

	const copyText = async (label: string, value: string) => {
		copyStatus = '';
		copyError = '';
		try {
			await navigator.clipboard.writeText(value);
			copyStatus = `${label} copied`;
		} catch (err) {
			copyError = err instanceof Error ? err.message : `Could not copy ${label.toLowerCase()}.`;
		}
	};

	const submitApproval = async ({
		requestID,
		action
	}: {
		requestID: string;
		action: ApprovalAction;
	}) => {
		if (
			approvalSubmitting[requestID] ||
			resolveApproval.pending > 0 ||
			resolvedApprovals[requestID]
		) {
			return;
		}
		const text = replyText[requestID]?.trim() ?? '';
		if (action === 'reply' && !text) {
			approvalError[requestID] = 'Reply text is required.';
			return;
		}

		approvalSubmitting[requestID] = true;
		approvalError[requestID] = '';
		const res = await resolveApproval({
			id,
			request_id: requestID,
			action,
			text
		});
		if (res.ok) {
			replyOpen[requestID] = false;
			replyText[requestID] = '';
			approvalError[requestID] = '';
		} else {
			approvalSubmitting[requestID] = false;
			approvalError[requestID] = res.error;
		}
	};

	const onkeydown = (e: KeyboardEvent) => {
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			send();
		}
	};
</script>

<section>
	<div class="mb-4 flex flex-wrap items-center justify-between gap-2">
		<div>
			<h1 class="text-xl font-semibold">Session</h1>
			<p class="text-xs text-gray-500">{id}</p>
		</div>
		<div class="flex flex-wrap items-center justify-end gap-3 text-sm">
			<span
				class="rounded-full px-2 py-0.5 text-xs font-medium {status === 'live'
					? 'bg-green-100 text-green-700'
					: status === 'error'
						? 'bg-red-100 text-red-700'
						: 'bg-gray-100 text-gray-500'}"
			>
				{status}
			</span>
			<button
				class="text-gray-600 hover:underline disabled:text-gray-300 disabled:no-underline"
				onclick={() => copyText('Transcript', transcriptText())}
				disabled={events.length === 0}
			>
				Copy transcript
			</button>
			<button
				class="text-gray-600 hover:underline disabled:text-gray-300 disabled:no-underline"
				onclick={() => copyText('Debug JSON', debugJSON())}
				disabled={events.length === 0}
			>
				Copy debug JSON
			</button>
			<button class="text-blue-600 hover:underline" onclick={reconnect}> Reconnect </button>
			{#if !done && !streamError}
				<button
					onclick={cancel}
					disabled={cancelSession.pending > 0}
					class="rounded bg-red-600 px-3 py-1 text-xs font-medium text-white disabled:opacity-50"
				>
					{cancelSession.pending > 0 ? 'Stopping…' : 'Stop'}
				</button>
			{/if}
			{#if done}
				<button
					onclick={deleteCurrentSession}
					disabled={deleteSession.pending > 0}
					class="rounded border border-red-300 px-3 py-1 text-xs font-medium text-red-700 disabled:opacity-50"
				>
					{deleteSession.pending > 0 ? 'Deleting…' : 'Delete'}
				</button>
			{/if}
		</div>
	</div>

	{#if cancelError}
		<p class="mb-3 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			{cancelError}
		</p>
	{/if}

	{#if deleteError}
		<p class="mb-3 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			{deleteError}
		</p>
	{/if}

	{#if streamError}
		<p class="mb-3 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			Stream error: {streamError}
		</p>
	{/if}

	{#if copyStatus}
		<p class="mb-3 rounded border border-green-200 bg-green-50 p-3 text-sm text-green-700">
			{copyStatus}
		</p>
	{/if}

	{#if copyError}
		<p class="mb-3 rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
			{copyError}
		</p>
	{/if}

	<div bind:this={logEl} class="max-h-[70vh] space-y-2 overflow-y-auto">
		{#each events as event (event.seq)}
			{#if event.type === 'session_started'}
				{@const p = event.payload as SessionStartedPayload}
				<div class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm">
					<p class="font-medium">Session started</p>
					<dl class="mt-1 space-y-0.5 text-gray-600">
						<div><span class="text-gray-400">repo:</span> {p.repo ?? '—'}</div>
						<div><span class="text-gray-400">agent:</span> {p.agent ?? '—'}</div>
						<div><span class="text-gray-400">branch:</span> {p.branch ?? '—'}</div>
						{#if p.prompt}
							<div class="whitespace-pre-wrap">
								<span class="text-gray-400">prompt:</span>
								{p.prompt}
							</div>
						{/if}
					</dl>
				</div>
			{:else if event.type === 'output'}
				<div class="rounded-lg border border-gray-200 p-3">
					<MarkdownOutput source={(event.payload as OutputPayload).text} />
				</div>
			{:else if event.type === 'input_received'}
				<div
					class="ml-8 rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm whitespace-pre-wrap"
				>
					{(event.payload as InputReceivedPayload).text}
				</div>
			{:else if event.type === 'awaiting_approval'}
				{@const p = event.payload as ApprovalPayload}
				{@const resolved = resolvedApprovals[p.request_id]}
				{@const submitting = approvalSubmitting[p.request_id]}
				<div
					class="rounded-lg border p-3 text-sm {resolved
						? 'border-gray-200 bg-gray-50'
						: 'border-amber-300 bg-amber-50'}"
				>
					<div class="flex items-start justify-between gap-3">
						<div>
							<p class="font-medium {resolved ? 'text-gray-700' : 'text-amber-800'}">
								{resolved ? 'Approval resolved' : 'Awaiting approval'}
							</p>
							<p class={resolved ? 'text-gray-600' : 'text-amber-700'}>
								{p.command || p.tool_name || 'tool request'}
							</p>
						</div>
						{#if resolved}
							<span class="rounded-full bg-gray-200 px-2 py-0.5 text-xs font-medium text-gray-600">
								{resolved.action}
							</span>
						{/if}
					</div>
					{#if p.reason}
						<p class="mt-2 text-xs {resolved ? 'text-gray-500' : 'text-amber-700'}">{p.reason}</p>
					{/if}
					{#if p.agent || p.tool_name}
						<p class="mt-1 text-xs {resolved ? 'text-gray-400' : 'text-amber-600'}">
							{p.agent ?? 'agent'}{p.tool_name ? ` · ${p.tool_name}` : ''}
						</p>
					{/if}
					{#if p.tool_input}
						<pre
							class="mt-2 overflow-x-auto rounded p-2 text-xs {resolved
								? 'bg-gray-100 text-gray-600'
								: 'bg-amber-100/60'}">{JSON.stringify(p.tool_input, null, 2)}</pre>
					{/if}
					{#if resolved}
						{#if resolved.text}
							<p class="mt-2 whitespace-pre-wrap text-xs text-gray-500">{resolved.text}</p>
						{/if}
					{:else}
						<div class="mt-3 flex flex-wrap gap-2">
							<button
								onclick={() => submitApproval({ requestID: p.request_id, action: 'approve' })}
								disabled={submitting || resolveApproval.pending > 0}
								class="rounded bg-green-600 px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50"
							>
								Approve
							</button>
							<button
								onclick={() => submitApproval({ requestID: p.request_id, action: 'deny' })}
								disabled={submitting || resolveApproval.pending > 0}
								class="rounded bg-red-600 px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50"
							>
								Deny
							</button>
							<button
								onclick={() => (replyOpen[p.request_id] = !replyOpen[p.request_id])}
								disabled={submitting || resolveApproval.pending > 0}
								class="rounded border border-amber-300 bg-white px-3 py-1.5 text-xs font-medium text-amber-800 disabled:opacity-50"
							>
								Reply
							</button>
						</div>
						{#if replyOpen[p.request_id]}
							<div class="mt-3">
								<textarea
									bind:value={replyText[p.request_id]}
									rows="2"
									placeholder="Reply with instructions…"
									class="w-full rounded border border-amber-300 bg-white p-2 text-sm"></textarea>
								<button
									onclick={() => submitApproval({ requestID: p.request_id, action: 'reply' })}
									disabled={submitting ||
										resolveApproval.pending > 0 ||
										!replyText[p.request_id]?.trim()}
									class="mt-2 rounded bg-amber-700 px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50"
								>
									{submitting || resolveApproval.pending > 0 ? 'Sending…' : 'Send reply'}
								</button>
							</div>
						{/if}
						{#if approvalError[p.request_id]}
							<p class="mt-2 text-xs text-red-600">{approvalError[p.request_id]}</p>
						{/if}
					{/if}
				</div>
			{:else if event.type === 'approval_resolved'}
				{@const p = event.payload as ApprovalResolvedPayload}
				<p class="text-xs text-gray-500">approval {p.action}{p.text ? `: ${p.text}` : ''}</p>
			{:else if event.type === 'tool_result'}
				{@const p = event.payload as ToolResultPayload}
				<div
					class="rounded-lg border p-3 text-sm {p.status === 'success'
						? 'border-emerald-200 bg-emerald-50'
						: 'border-red-200 bg-red-50'}"
				>
					<div class="flex flex-wrap items-start justify-between gap-2">
						<div>
							<p
								class={p.status === 'success'
									? 'font-medium text-emerald-800'
									: 'font-medium text-red-800'}
							>
								Tool result
							</p>
							<p class={p.status === 'success' ? 'text-emerald-700' : 'text-red-700'}>
								{p.tool_name || 'tool'}{p.tool_use_id ? ` · ${p.tool_use_id}` : ''}
							</p>
						</div>
						<span
							class="rounded-full px-2 py-0.5 text-xs font-medium {p.status === 'success'
								? 'bg-emerald-100 text-emerald-700'
								: 'bg-red-100 text-red-700'}"
						>
							{p.status}
						</span>
					</div>
					<p
						class={p.status === 'success'
							? 'mt-1 text-xs text-emerald-700'
							: 'mt-1 text-xs text-red-700'}
					>
						{p.duration_ms != null ? `${p.duration_ms} ms` : ''}
						{p.approval_request_id
							? `${p.duration_ms != null ? ' · ' : ''}approval ${p.approval_request_id}`
							: ''}
						{p.redacted ? ' · redacted' : ''}{p.truncated ? ' · truncated' : ''}
					</p>
					{#if p.tool_input}
						<pre class="mt-2 overflow-x-auto rounded bg-white/70 p-2 text-xs">{JSON.stringify(
								p.tool_input,
								null,
								2
							)}</pre>
					{/if}
					{#if p.response}
						<pre class="mt-2 overflow-x-auto rounded bg-white/70 p-2 text-xs">{JSON.stringify(
								p.response,
								null,
								2
							)}</pre>
					{/if}
					{#if p.error}
						<p class="mt-2 whitespace-pre-wrap text-xs text-red-700">{p.error}</p>
					{/if}
				</div>
			{:else if event.type === 'usage'}
				{@const p = event.payload as UsagePayload}
				<p class="text-xs text-gray-400">
					usage · {p.tokens?.total_billable ?? 0} tokens{p.cost
						? ` · $${p.cost.total_usd.toFixed(4)}`
						: ''}
				</p>
			{:else if event.type === 'done'}
				<p class="text-sm text-gray-500">
					Session finished ({(event.payload as DonePayload).reason})
				</p>
			{:else if event.type === 'error'}
				<p class="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700">
					{(event.payload as ErrorPayload).message}
				</p>
			{:else}
				<pre class="overflow-x-auto rounded bg-gray-100 p-2 text-xs text-gray-500">{JSON.stringify(
						event.payload
					)}</pre>
			{/if}
		{/each}

		{#if events.length === 0 && !streamError}
			<p class="text-sm text-gray-500">Waiting for events…</p>
		{/if}
	</div>

	{#if done}
		<p class="mt-4 text-sm text-gray-500">Session finished.</p>
	{:else}
		<div class="mt-4">
			<div class="flex items-end gap-2">
				<textarea
					bind:value={text}
					{onkeydown}
					rows="2"
					placeholder="Send a message… (Enter to send, Shift+Enter for newline)"
					class="w-full rounded border p-2 text-sm"></textarea>
				<button
					onclick={send}
					disabled={!text.trim() || sendInput.pending > 0}
					class="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
				>
					{sendInput.pending > 0 ? 'Sending…' : 'Send'}
				</button>
			</div>
			{#if sendError}
				<p class="mt-1 text-xs text-red-600">{sendError}</p>
			{/if}
		</div>
	{/if}
</section>
