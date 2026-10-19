<script lang="ts">
	import { onMount, tick, untrack } from 'svelte';
	import { SvelteMap } from 'svelte/reactivity';

	import type { components } from '@tethon/hub-sdk/hub-openapi';
	import type {
		CodexHistoryTurn,
		CodexSessionState,
		CodexRequestPayload,
		Event,
		CodexEventPayload
	} from '@tethon/hub-sdk/protocol';
	import type { SessionSummary } from '$lib/types';
	import {
		getModels,
		getSettings,
		getNativeSession,
		getNativeHistory,
		respondNativeRequest,
		changeNativeConversation
	} from '$lib/remote-functions/codex.remote';
	import { sendInput, cancelSession } from '$lib/remote-functions/sessions.remote';

	import { object, list, text as string, label, nativeText, mergeTimeline } from '$lib/codex';
	import NativeItem from './native-item.svelte';
	import NativeRequest from './native-request.svelte';
	import NativeDiff from './native-diff.svelte';
	import CodexSettings from './codex-settings.svelte';

	let { session }: { session: SessionSummary } = $props();

	const id = untrack(() => session.id);
	const cacheKey = `tethon:native:${id}`;

	let events = $state<Event[]>([]);
	let history = $state<CodexHistoryTurn[]>([]);
	let nextCursor = $state('');
	let nativeState = $state<CodexSessionState | undefined>(untrack(() => session.codex));
	let snapshot = $state<components['schemas']['CodexSessionResponse']>();
	let snapshotCursor = $state(0);
	let cursor = $state(0);
	let streamConnected = $state(false);
	let freshSnapshot = $state(false);
	let streamError = $state('');
	let loadError = $state('');
	let actionError = $state('');
	let loadingHistory = $state(false);
	let lifecycleNotice = $state('');
	let draft = $state('');
	let inputMode = $state<'steer' | 'queue'>('steer');
	let model = $state('');
	let effort = $state('');
	let permissionProfile = $state('');
	let storageNotice = $state('');
	let log = $state<HTMLElement>();
	let follow = $state(true);
	let restoredScroll = 0;
	let initialized = $state(false);
	let disposed = false;
	let controller: AbortController | undefined;
	let latestStateSeq = 0;
	let messageAttempt: { fingerprint: string; requestID: string } | undefined;

	const models = $derived(getModels({ host_id: session.host_id, agent: 'codex-cli' }));
	const workingCWD = $derived(
		nativeState?.cwd || `${session.repo}${session.cwd ? `/${session.cwd}` : ''}`
	);
	const settings = $derived(
		getSettings({
			host_id: session.host_id,
			cwd: workingCWD
		})
	);
	const choices = $derived(models.current?.ok ? (models.current.value.models ?? []) : []);
	const efforts = $derived(choices.find((choice) => choice.id === model)?.reasoning_efforts ?? []);
	const permissionProfiles = $derived(
		settings.current?.ok
			? (settings.current.value.permission_profiles?.profiles ?? []).filter(
					(profile) => profile.allowed
				)
			: []
	);

	const timeline = $derived(
		nativeState?.conversation_state === 'unavailable' ? [] : mergeTimeline({ history, events })
	);

	const active = $derived(nativeState?.turn_state === 'in_progress');
	const available = $derived(
		freshSnapshot &&
			nativeState?.connection_state === 'connected' &&
			nativeState.conversation_state === 'ready'
	);
	const archived = $derived(nativeState?.conversation_state === 'archived');
	const cleanupAllowed = $derived(
		snapshot?.workspace_owned === true &&
			!active &&
			(archived || nativeState?.conversation_state === 'unavailable')
	);
	const queueAvailable = $derived(snapshot?.queue_available === true);
	const queue = $derived(list(snapshot?.queue));
	const requests = $derived.by(() => {
		const pending = new SvelteMap<string, CodexRequestPayload>();
		for (const request of snapshot?.pending_requests ?? [])
			pending.set(`${request.connection_id}/${request.request_key}`, request);
		for (const event of events) {
			if (event.seq <= snapshotCursor) continue;
			const payload = object(event.payload);
			const key = `${string(payload.connection_id)}/${string(payload.request_key)}`;
			if (event.type === 'codex_request') pending.set(key, event.payload as CodexRequestPayload);
			if (event.type === 'codex_request_resolved') pending.delete(key);
		}
		return [...pending.values()].filter(
			(request) => request.connection_id === nativeState?.connection_id
		);
	});
	const usage = $derived.by(() => {
		const event = events.findLast(
			(e) =>
				e.type === 'codex_event' &&
				(e.payload as CodexEventPayload).method === 'thread/tokenUsage/updated'
		);
		const params = object((event?.payload as CodexEventPayload | undefined)?.params);
		return object(object(params.tokenUsage).total);
	});
	const inputFailures = $derived(
		events
			.filter(
				(event) =>
					event.type === 'codex_event' &&
					(event.payload as CodexEventPayload).method === 'tethon/inputError'
			)
			.slice(-3)
	);

	const notices = $derived(
		events
			.filter(
				(e) =>
					e.type === 'error' ||
					(e.type === 'codex_event' &&
						((e.payload as CodexEventPayload).truncated ||
							['warning', 'configWarning', 'error'].includes(
								(e.payload as CodexEventPayload).method
							)))
			)
			.slice(-5)
	);

	const persist = () => {
		try {
			if (events.length > 3000) {
				localStorage.removeItem(cacheKey);
				storageNotice =
					'The local event cache is full. Reload uses native history and host replay.';
				return;
			}
			localStorage.setItem(cacheKey, JSON.stringify({ events, history, nextCursor, cursor }));
		} catch {
			try {
				localStorage.removeItem(cacheKey);
			} catch {
				/* Storage can be disabled. */
			}
			storageNotice =
				'Local event storage is unavailable. Reload uses native history and host replay.';
		}
	};

	const savePosition = () => {
		if (!log || !initialized) return;
		follow = log.scrollHeight - log.clientHeight - log.scrollTop < 80;
		try {
			localStorage.setItem(`tethon:scroll:${id}`, JSON.stringify({ top: log.scrollTop, follow }));
		} catch {
			/* Keep the live view usable. */
		}
	};

	const refreshSnapshot = async () => {
		try {
			const query = getNativeSession({ id });
			await query.refresh();
			const result = query.current;
			if (result?.ok) {
				const seq = result.value.seq;
				if (seq < snapshotCursor) return;
				snapshot = result.value;
				if (seq >= latestStateSeq) nativeState = result.value.state;
				snapshotCursor = seq;
				freshSnapshot = true;
				loadError = '';
			} else if (result) loadError = result.error;
		} catch (error) {
			loadError = error instanceof Error ? error.message : 'Could not read native state.';
		}
	};

	const loadHistory = async (older = false) => {
		if (loadingHistory) return;
		loadingHistory = true;
		const previousHeight = log?.scrollHeight ?? 0;
		try {
			const query = getNativeHistory({ id, cursor: older ? nextCursor : undefined });
			await query.refresh();
			const result = query.current;
			if (!result?.ok) {
				loadError = result?.error || 'Could not load native history.';
				return;
			}
			const page: CodexHistoryTurn[] = (result.value.turns ?? [])
				.map((turn) => ({ ...turn, items: turn.items ?? [] }))
				.reverse();
			const incoming = new Set(page.map((turn) => turn.id));
			history = older
				? [...page, ...history.filter((turn) => !incoming.has(turn.id))]
				: [...history.filter((turn) => !incoming.has(turn.id)), ...page];
			if (older || !nextCursor) nextCursor = result.value.next_cursor ?? '';
			persist();
			if (older) {
				follow = false;
				await tick();
				if (log) log.scrollTop += log.scrollHeight - previousHeight;
			}
		} catch (error) {
			loadError = error instanceof Error ? error.message : 'Could not load native history.';
		} finally {
			loadingHistory = false;
		}
	};

	const consume = async () => {
		controller?.abort();
		const activeController = new AbortController();
		controller = activeController;
		freshSnapshot = false;
		await refreshSnapshot();
		try {
			const response = await fetch(`/sessions/${id}/stream?from=${cursor}`, {
				signal: activeController.signal,
				headers: { accept: 'text/event-stream' }
			});
			if (!response.ok || !response.body)
				throw new Error(`Event stream returned ${response.status}.`);
			streamConnected = true;
			streamError = '';
			const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
			let buffer = '';
			for (;;) {
				const chunk = await reader.read();
				if (chunk.done) break;
				buffer += chunk.value.replaceAll('\r\n', '\n');
				if (buffer.length > 1024 * 1024)
					throw new Error(
						'The event frame exceeds the delivery limit. Reconnect to read native history.'
					);
				let boundary: number;
				while ((boundary = buffer.indexOf('\n\n')) >= 0) {
					const frame = buffer.slice(0, boundary);
					buffer = buffer.slice(boundary + 2);
					const data = frame
						.split('\n')
						.filter((line) => line.startsWith('data:'))
						.map((line) => line.slice(5).trimStart())
						.join('\n');
					if (!data) continue;
					const event = JSON.parse(data) as Event;
					if (
						!Number.isSafeInteger(event.seq) ||
						event.seq <= cursor ||
						event.session_id !== id.slice(id.indexOf('/') + 1)
					)
						continue;
					events.push(event);
					cursor = event.seq;
					if (event.type === 'codex_state' && event.seq > snapshotCursor) {
						nativeState = event.payload as CodexSessionState;
						latestStateSeq = event.seq;
					}
					if (
						event.type === 'codex_request_resolved' ||
						(event.type === 'codex_event' &&
							['turn/completed', 'thread/queue/changed'].includes(
								(event.payload as CodexEventPayload).method
							))
					)
						void refreshSnapshot();
				}
				persist();
			}
		} catch (error) {
			if (!activeController.signal.aborted)
				streamError = error instanceof Error ? error.message : 'Disconnected from the host.';
		} finally {
			if (controller === activeController) {
				streamConnected = false;
				freshSnapshot = false;
				controller = undefined;
			}
		}
	};

	const send = async () => {
		const message = draft.trim();
		if (!message || !available || sendInput.pending) return;
		const body = {
			id,
			text: message,
			mode: active ? inputMode : ('start' as const),
			turn_id: active ? nativeState?.turn_id : undefined,
			requested_model: !active && model ? model : undefined,
			reasoning_effort: !active && effort ? effort : undefined,
			codex_permission_profile: !active && permissionProfile ? permissionProfile : undefined
		};
		const fingerprint = JSON.stringify(body);
		if (!messageAttempt || messageAttempt.fingerprint !== fingerprint)
			messageAttempt = { fingerprint, requestID: crypto.randomUUID() };

		try {
			localStorage.setItem(`tethon:attempt:${id}`, JSON.stringify(messageAttempt));
		} catch {
			/* Keep retries stable in memory. */
		}

		actionError = '';
		try {
			const result = await sendInput({
				...body,
				request_id: messageAttempt.requestID
			});
			if (!result.ok) {
				actionError = result.error;
				return;
			}
			if (draft.trim() === message) draft = '';
			messageAttempt = undefined;
			try {
				localStorage.removeItem(`tethon:attempt:${id}`);
			} catch {
				/* The acknowledged attempt is cleared in memory. */
			}
			await refreshSnapshot();
		} catch (error) {
			actionError =
				error instanceof Error
					? error.message
					: 'Could not send the message. Retry uses the same request key.';
		}
	};

	const stop = async () => {
		if (!active || !available) return;
		try {
			const result = await cancelSession({ id, turn_id: nativeState?.turn_id });
			if (!result.ok) actionError = result.error;
			else {
				actionError = '';
				await refreshSnapshot();
			}
		} catch (error) {
			actionError = error instanceof Error ? error.message : 'Could not stop this turn.';
		}
	};

	const respond = async (request: CodexRequestPayload, result: unknown) => {
		const response = await respondNativeRequest({
			id,
			request_key: request.request_key,
			connection_id: request.connection_id,
			result
		});
		await refreshSnapshot();
		if (!response.ok) throw new Error(response.error);
	};

	const changeConversation = async (action: 'archive' | 'unarchive' | 'delete' | 'cleanup') => {
		if (!freshSnapshot || active || changeNativeConversation.pending) return;

		actionError = '';
		lifecycleNotice = '';
		try {
			const result = await changeNativeConversation({ id, action });
			if (!result.ok) {
				actionError = result.error;
				return;
			}

			lifecycleNotice = {
				archive: 'Conversation archived. Working files are kept.',
				unarchive: 'Conversation restored.',
				delete: 'Native conversation deleted. Working files are kept.',
				cleanup: 'The isolated workspace cleanup is complete.'
			}[action];
			await refreshSnapshot();
			if (action === 'delete') {
				events = [];
				history = [];
				nextCursor = '';
				cursor = Math.max(cursor, snapshotCursor);
				persist();
			}
		} catch (error) {
			actionError = error instanceof Error ? error.message : 'Could not complete this action.';
		}
	};

	$effect(() => {
		if (initialized) {
			try {
				localStorage.setItem(`tethon:draft:${id}`, draft);
				localStorage.setItem(
					`tethon:turn-settings:${id}`,
					JSON.stringify({ model, effort, permissionProfile })
				);
			} catch {
				/* Preserve in memory. */
			}
		}
	});

	$effect(() => {
		if (timeline.length && follow && log)
			void tick().then(() => log?.scrollTo({ top: log.scrollHeight }));
	});

	onMount(() => {
		try {
			draft = localStorage.getItem(`tethon:draft:${id}`) ?? '';
			const attempt = JSON.parse(localStorage.getItem(`tethon:attempt:${id}`) || 'null');
			if (typeof attempt?.fingerprint === 'string' && typeof attempt.requestID === 'string')
				messageAttempt = attempt;
			const savedSettings = JSON.parse(
				localStorage.getItem(`tethon:turn-settings:${id}`) || 'null'
			);
			if (savedSettings) {
				model = string(savedSettings.model);
				effort = string(savedSettings.effort);
				permissionProfile = string(savedSettings.permissionProfile);
			}

			const cached = JSON.parse(localStorage.getItem(cacheKey) || 'null');
			if (cached && Array.isArray(cached.events) && Number.isSafeInteger(cached.cursor)) {
				events = cached.events;
				cursor = cached.cursor;
				history = cached.history ?? [];
				nextCursor = cached.nextCursor ?? '';
			}
			const scroll = JSON.parse(localStorage.getItem(`tethon:scroll:${id}`) || 'null');
			if (scroll) {
				restoredScroll = scroll.top;
				follow = scroll.follow;
			}
		} catch {
			cursor = 0;
			events = [];
		}
		initialized = true;
		void (async () => {
			await Promise.all([refreshSnapshot(), loadHistory()]);
			await tick();
			if (log && !follow) log.scrollTop = restoredScroll;
			while (!disposed) {
				await consume();
				if (disposed) break;
				await new Promise((resolve) => setTimeout(resolve, 1500));
			}
		})();
		const timer = setInterval(() => {
			if (!document.hidden) void refreshSnapshot();
		}, 5000);
		const focus = () => {
			void refreshSnapshot();
			void loadHistory();
		};
		window.addEventListener('focus', focus);
		return () => {
			disposed = true;
			controller?.abort();
			clearInterval(timer);
			window.removeEventListener('focus', focus);
		};
	});
</script>

<section class="flex h-[calc(100dvh-8rem)] min-h-[30rem] min-w-0 flex-col">
	<div class="mb-4 flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<h1 class="text-xl font-semibold">Codex conversation</h1>
			<p class="mt-1 text-xs break-all text-gray-500">
				{workingCWD}
			</p>
			<p class="mt-1 text-xs break-all text-gray-500">
				{nativeState?.thread_id || 'Connecting to Codex…'}
			</p>
		</div>
		<div class="flex flex-wrap items-center gap-2">
			<span class="rounded-full bg-gray-100 px-2 py-1 text-xs text-gray-600">
				{label(
					nativeState?.conversation_state !== 'ready'
						? nativeState?.conversation_state
						: nativeState?.turn_state
				) || 'Loading'}
			</span>
			<span class="text-xs text-gray-500">
				{streamConnected ? 'Live' : 'Reconnecting'} · {label(nativeState?.connection_state)}
			</span>
			<button
				class="cursor-pointer rounded border px-3 py-1.5 text-xs disabled:opacity-50"
				disabled={!active || !available || cancelSession.pending > 0}
				onclick={stop}>Stop turn</button
			>
		</div>
	</div>
	{#if session.model || session.requested_model}
		<p class="mb-3 text-xs text-gray-500">
			{session.model || 'Agent default'}{session.requested_model
				? ` · requested ${session.requested_model}`
				: ''}
		</p>
	{/if}

	<CodexSettings hostId={session.host_id} cwd={workingCWD} />

	{#if nativeState?.conversation_state !== 'unavailable' || cleanupAllowed}
		<details class="mb-3 shrink-0 rounded border border-gray-200 p-3">
			<summary class="cursor-pointer text-sm font-medium">Conversation actions</summary>

			{#if nativeState?.conversation_state !== 'unavailable'}
				<p class="mt-2 text-xs text-gray-600">
					Archive keeps working files. Restore opens the same conversation.
				</p>
				<div class="mt-3 flex flex-wrap gap-2">
					<button
						class="cursor-pointer rounded border px-3 py-2 text-sm disabled:opacity-50"
						disabled={!freshSnapshot || active || changeNativeConversation.pending > 0}
						onclick={() => changeConversation(archived ? 'unarchive' : 'archive')}
					>
						{archived ? 'Restore conversation' : 'Archive conversation'}
					</button>
				</div>

				<div class="mt-4 border-t border-red-100 pt-3">
					<p class="text-xs text-gray-600">
						Delete removes native conversation history. Working files are kept.
					</p>
					<button
						class="mt-2 cursor-pointer rounded border border-red-200 px-3 py-2 text-sm text-red-700 disabled:opacity-50"
						disabled={!freshSnapshot || active || changeNativeConversation.pending > 0}
						onclick={() => changeConversation('delete')}
					>
						Delete conversation history
					</button>
				</div>
			{/if}

			{#if cleanupAllowed}
				<div class="mt-4 border-t border-red-100 pt-3">
					<p class="text-xs text-gray-600">
						Remove this Tethon isolated workspace. The host rejects changed files.
					</p>
					<button
						class="mt-2 cursor-pointer rounded border border-red-200 px-3 py-2 text-sm text-red-700 disabled:opacity-50"
						disabled={!freshSnapshot || changeNativeConversation.pending > 0}
						onclick={() => changeConversation('cleanup')}
					>
						Remove isolated workspace
					</button>
				</div>
			{/if}
		</details>
	{/if}

	{#if lifecycleNotice}
		<p role="status" class="mb-3 text-sm text-gray-600">{lifecycleNotice}</p>
	{/if}

	{#if streamError || loadError}
		<div
			role="alert"
			class="mb-3 rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900"
		>
			{loadError || streamError}
			<button
				class="ml-3 cursor-pointer underline"
				onclick={() => {
					void refreshSnapshot();
					void loadHistory();
					controller?.abort();
				}}>Reconnect</button
			>
		</div>
	{/if}
	{#if storageNotice}
		<p class="mb-3 text-xs text-amber-800">{storageNotice}</p>
	{/if}
	<!-- svelte-ignore a11y_no_noninteractive_tabindex (This scroll region supports keyboard scrolling.) -->
	<div
		bind:this={log}
		onscroll={savePosition}
		class="min-h-24 flex-1 space-y-4 overflow-y-auto overscroll-contain pr-1"
		tabindex="0"
		role="region"
		aria-label="Conversation timeline"
	>
		{#if nextCursor}
			<button
				disabled={loadingHistory}
				onclick={() => loadHistory(true)}
				class="w-full cursor-pointer rounded border py-2 text-sm text-blue-700 disabled:opacity-50"
			>
				{loadingHistory ? 'Loading…' : 'Load earlier turns'}
			</button>
		{/if}
		{#each timeline as turn (turn.id)}
			<section class="space-y-3" aria-label="Conversation turn">
				<div class="flex items-center gap-2 text-xs text-gray-500">
					<span>{label(turn.status)} </span>
					<span class="h-px flex-1 bg-gray-100"> </span>
				</div>
				{#each turn.items as item, index (string(item.id) || `${turn.id}-${index}`)}
					<NativeItem {item} turnId={turn.id} />
				{/each}
				{#if turn.plan?.length}
					<details class="rounded border border-gray-200 p-3" open>
						<summary class="cursor-pointer text-sm font-medium">Turn plan</summary>
						<ol class="mt-2 space-y-1">
							{#each turn.plan as step, index (index)}
								<li class="text-sm">
									{step.status === 'completed' ? '✓' : step.status === 'inProgress' ? '◦' : '·'}
									{string(step.step)}
									<span class="text-xs text-gray-500">{label(step.status)} </span>
								</li>
							{/each}
						</ol>
					</details>
				{/if}
				{#if turn.diff}
					<details class="rounded border border-gray-200 p-3">
						<summary class="cursor-pointer text-sm font-medium">Turn diff</summary>
						<NativeDiff diff={turn.diff} />
					</details>
				{/if}
				{#if turn.error}
					<p role="alert" class="rounded bg-red-50 p-3 text-sm text-red-700">
						{nativeText(turn.error) || 'The turn failed.'}
					</p>
				{/if}
			</section>
		{:else}
			<p class="py-6 text-sm text-gray-500">
				{loadingHistory
					? 'Loading native history…'
					: nativeState?.conversation_state === 'unavailable'
						? 'This native conversation is unavailable.'
						: 'Send a message to continue this conversation.'}
			</p>
		{/each}
	</div>
	{#if !follow}
		<button
			class="mt-2 cursor-pointer text-sm text-blue-700"
			onclick={() => {
				follow = true;
				log?.scrollTo({ top: log.scrollHeight });
			}}>Jump to latest</button
		>
	{/if}
	{#if Object.keys(usage).length}
		<p class="mt-3 text-xs text-gray-500">
			Usage · {Number(usage.totalTokens ?? 0).toLocaleString()} tokens{typeof usage.inputTokens ===
			'number'
				? ` · ${usage.inputTokens.toLocaleString()} input`
				: ''}{typeof usage.outputTokens === 'number'
				? ` · ${usage.outputTokens.toLocaleString()} output`
				: ''}
		</p>
	{/if}
	{#each inputFailures as failure (failure.seq)}
		<div
			role="alert"
			class="mt-3 shrink-0 rounded border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900"
		>
			<h2 class="font-medium">Message needs review</h2>
			<p class="mt-2 whitespace-pre-wrap">
				{nativeText((failure.payload as CodexEventPayload).params) ||
					'The host could not confirm the first message.'}
			</p>
			<p class="mt-2">
				The conversation was kept. Check native history or the host terminal before sending another
				message.
			</p>
			<button
				type="button"
				class="mt-2 cursor-pointer underline disabled:opacity-50"
				disabled={loadingHistory}
				onclick={() => loadHistory()}
			>
				Reload native history
			</button>
		</div>
	{/each}

	{#if notices.length}
		<details class="mt-3 shrink-0 rounded border border-amber-200 p-2">
			<summary class="cursor-pointer text-xs text-amber-900">
				Codex notices ({notices.length})</summary
			>
			{#each notices as notice (notice.seq)}
				<p class="rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
					{notice.type === 'error'
						? nativeText(notice.payload)
						: (notice.payload as CodexEventPayload).truncated
							? 'A native update exceeded the display limit. Reload history to read the current items.'
							: nativeText((notice.payload as CodexEventPayload).params) ||
								'Codex reported a runtime notice.'}
				</p>
			{/each}
		</details>
	{/if}

	<div class="mt-4 space-y-3">
		{#each requests as request (`${request.connection_id}/${request.request_key}`)}
			<NativeRequest
				{request}
				disabled={!available}
				onrespond={(result) => respond(request, result)}
			/>
		{/each}
	</div>
	{#if queue.length}
		<details class="mt-4 rounded border border-gray-200 p-3" open>
			<summary class="cursor-pointer text-sm font-medium">Queued input ({queue.length})</summary>
			<ol class="mt-2 space-y-2">
				{#each queue as entry, index (index)}
					<li class="text-sm whitespace-pre-wrap">
						{nativeText(object(entry).input) || nativeText(entry) || 'Queued native input'}
					</li>
				{/each}
			</ol>
		</details>
	{/if}
	<form
		class="mt-4 shrink-0 rounded-lg border border-gray-200 bg-white p-3"
		onsubmit={(event) => {
			event.preventDefault();
			void send();
		}}
	>
		{#if !active && nativeState?.conversation_state === 'ready'}
			<details class="mb-3 rounded border border-gray-200 p-2">
				<summary class="cursor-pointer text-xs font-medium">Next turn settings</summary>
				<div class="mt-3 grid gap-3 sm:grid-cols-2">
					<label class="block text-xs text-gray-600">
						<span>Model</span>
						<select
							bind:value={model}
							onchange={() => {
								effort = '';
							}}
							class="mt-1 w-full rounded border p-2 text-sm"
						>
							<option value="">Native default</option>
							{#each choices as choice (choice.id)}
								<option value={choice.id}>{choice.label}</option>
							{/each}
						</select>
					</label>
					{#if efforts.length}
						<label class="block text-xs text-gray-600">
							<span>Reasoning effort</span>
							<select bind:value={effort} class="mt-1 w-full rounded border p-2 text-sm">
								<option value="">Model default</option>
								{#each efforts as choice (choice.id)}
									<option value={choice.id}>{choice.id}</option>
								{/each}
							</select>
						</label>
					{/if}
					{#if permissionProfiles.length}
						<label class="block text-xs text-gray-600">
							<span>Permission profile</span>
							<select bind:value={permissionProfile} class="mt-1 w-full rounded border p-2 text-sm">
								<option value="">Native default</option>
								{#each permissionProfiles as profile (profile.id)}
									<option value={profile.id}>{profile.id}</option>
								{/each}
							</select>
						</label>
					{/if}
				</div>
				<p class="mt-2 text-xs text-gray-500">
					Selected settings apply with the next message. Omitted settings keep native defaults.
				</p>
			</details>
		{/if}

		<label class="block text-xs font-medium text-gray-600" for="native-message">
			{active ? 'Message the active turn' : 'Continue the conversation'}
		</label>
		<textarea
			id="native-message"
			bind:value={draft}
			rows="3"
			class="mt-2 w-full resize-y rounded border p-2 text-sm"
			placeholder="Send a message…"
			onkeydown={(event) => {
				if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
					event.preventDefault();
					void send();
				}
			}}></textarea>
		<div class="mt-2 flex flex-wrap items-center justify-between gap-2">
			{#if active}
				<label class="text-xs text-gray-600">
					Send as<select bind:value={inputMode} class="ml-2 rounded border p-1.5 text-sm">
						<option value="steer">Steer this turn</option>
						{#if queueAvailable}
							<option value="queue">Queue next turn</option>
						{/if}
					</select>
				</label>
			{:else}
				<span class="text-xs text-gray-500">Enter to send · Shift+Enter for a new line</span>
			{/if}
			<button
				disabled={!draft.trim() ||
					!available ||
					sendInput.pending > 0 ||
					(inputMode === 'queue' && active && !queueAvailable)}
				class="cursor-pointer rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white disabled:cursor-default disabled:opacity-50"
			>
				{sendInput.pending
					? 'Sending…'
					: active && inputMode === 'queue'
						? 'Queue message'
						: active
							? 'Steer turn'
							: 'Send'}
			</button>
		</div>
		{#if !available}
			<p class="mt-2 text-xs text-amber-800">
				{nativeState?.conversation_state === 'archived'
					? 'This conversation is archived.'
					: nativeState?.conversation_state === 'unavailable'
						? 'This native conversation is unavailable. Your draft is saved.'
						: 'Waiting for the native Codex connection. Your draft is saved.'}
			</p>
		{/if}
		{#if actionError}
			<p role="alert" class="mt-2 text-sm text-red-700">{actionError}</p>
		{/if}
	</form>
</section>
