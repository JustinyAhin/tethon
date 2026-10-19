<script lang="ts">
	import MarkdownOutput from './markdown-output.svelte';
	import NativeDiff from './native-diff.svelte';

	import { object, list, text, label, nativeText, type NativeObject } from '$lib/codex';

	let { item, turnId }: { item: NativeObject; turnId: string } = $props();

	const kind = $derived(text(item.type));
	const changes = $derived(list(item.changes).map(object));

	const fileId = (path: string) => `file-${encodeURIComponent(turnId)}-${encodeURIComponent(path)}`;
</script>

{#if kind === 'agentMessage' || kind === 'plan'}
	<article class="rounded-lg border border-gray-200 p-4">
		<p class="mb-2 text-xs font-medium text-gray-500">{kind === 'plan' ? 'Plan' : 'Codex'}</p>
		<MarkdownOutput source={text(item.text)} />
	</article>
{:else if kind === 'userMessage'}
	<article class="ml-4 rounded-lg border border-blue-100 bg-blue-50 p-4">
		<p class="mb-2 text-xs font-medium text-blue-700">You</p>
		<p class="text-sm whitespace-pre-wrap">{nativeText(item.content)}</p>
		{#each list(item.content).filter((entry) => object(entry).type !== 'text') as entry, index (index)}
			<p class="mt-2 text-xs text-gray-500">
				{label(object(entry).type)} · {text(object(entry).path) ||
					text(object(entry).url) ||
					'Attached content'}
			</p>
		{/each}
	</article>
{:else if kind === 'commandExecution'}
	<details class="rounded-lg border border-gray-200 p-3" open={item.status === 'inProgress'}>
		<summary class="cursor-pointer text-sm">
			<span class="font-medium">Command</span>
			<span class="ml-2 text-xs text-gray-500">
				{label(item.status)}{typeof item.exitCode === 'number'
					? ` · exit ${item.exitCode}`
					: ''}{typeof item.durationMs === 'number' ? ` · ${item.durationMs} ms` : ''}
			</span>
			<code class="mt-2 block overflow-hidden text-ellipsis whitespace-nowrap">
				{text(item.command) || 'Command output'}
			</code>
		</summary>
		<p class="mt-2 text-xs break-all text-gray-500">{text(item.cwd)}</p>
		<code
			class="mt-2 block max-h-80 overflow-auto rounded bg-gray-50 p-3 text-xs whitespace-pre-wrap"
			>{text(item.aggregatedOutput) ||
				(item.status === 'inProgress' ? 'No output yet.' : 'No output.')}</code
		>
	</details>
{:else if kind === 'fileChange'}
	<article class="rounded-lg border border-gray-200 p-3">
		<p class="text-sm font-medium">
			File changes <span class="text-xs font-normal text-gray-500">{label(item.status)} </span>
		</p>
		<nav aria-label="Changed files" class="mt-2 flex flex-wrap gap-2">
			{#each changes as change, index (index)}
				<a class="text-xs break-all text-blue-700 underline" href="#{fileId(text(change.path))}">
					{text(change.path)}
				</a>
			{/each}
		</nav>
		{#each changes as change, index (index)}
			<details id={fileId(text(change.path))} class="mt-3 rounded border border-gray-200">
				<summary class="cursor-pointer p-2 text-xs break-all font-medium">
					{text(change.path)}
				</summary>
				<NativeDiff diff={text(change.diff)} />
			</details>
		{/each}
	</article>
{:else if kind === 'reasoning'}
	<details class="rounded-lg border border-gray-200 p-3">
		<summary class="cursor-pointer text-sm text-gray-600">Reasoning summary</summary>
		<div class="mt-2">
			<MarkdownOutput source={nativeText(item.summary) || text(item.streamedSummary)} />
		</div>
	</details>
{:else if kind === 'mcpToolCall' || kind === 'dynamicToolCall' || kind === 'functionCallOutput'}
	<details class="rounded-lg border border-gray-200 p-3">
		<summary class="cursor-pointer text-sm font-medium">
			{text(item.server)}{item.server ? ' / ' : ''}{text(item.tool) ||
				text(item.name) ||
				'Tool result'}
			<span class="text-xs font-normal text-gray-500">{label(item.status)} </span>
		</summary>
		<p class="mt-2 text-sm whitespace-pre-wrap">
			{nativeText(item.result) ||
				nativeText(item.contentItems) ||
				nativeText(item.output) ||
				'The tool returned structured content.'}
		</p>
		{#if item.error}
			<p class="mt-2 text-sm text-red-700">
				{nativeText(item.error) || 'The tool failed.'}
			</p>
		{/if}
	</details>
{:else if kind === 'collabAgentToolCall' || kind === 'subAgentActivity'}
	<article class="rounded-lg border border-gray-200 p-3 text-sm">
		<p class="font-medium">Agent activity · {label(item.tool) || label(item.kind)}</p>
		<p class="mt-1 text-xs break-all text-gray-500">
			{text(item.agentPath) || list(item.receiverThreadIds).map(text).join(', ')} · {label(
				item.status
			)}{item.model ? ` · ${text(item.model)}` : ''}
		</p>
		{#if item.prompt}
			<p class="mt-2 whitespace-pre-wrap">
				{text(item.prompt)}
			</p>
		{/if}

		{#each Object.entries(object(item.agentsStates)) as [agent, state] (agent)}
			<p class="mt-2 text-xs break-all">
				{agent}: {label(object(state).status)}{object(state).message
					? ` · ${text(object(state).message)}`
					: ''}
			</p>
		{/each}
	</article>
{:else if kind === 'webSearch'}
	<p class="rounded border border-gray-200 p-3 text-sm">
		Web search · {text(item.query) || nativeText(object(item.action).query) || 'Search completed'}
	</p>
{:else if kind === 'contextCompaction'}
	<p class="text-xs text-gray-500">Codex compacted the conversation history.</p>
{:else if kind === 'enteredReviewMode' || kind === 'exitedReviewMode'}
	<p class="text-sm text-gray-600">
		{label(kind)} · {text(item.review)}
	</p>
{:else if kind === 'imageView' || kind === 'imageGeneration'}
	<p class="rounded border border-gray-200 p-3 text-sm">
		Image · {text(item.path) || 'View this image in Codex.'}
	</p>
{:else}
	<p class="rounded border border-gray-200 p-3 text-sm text-gray-500">
		{label(kind) || 'Native item'} · This item is available in Codex.
	</p>
{/if}
