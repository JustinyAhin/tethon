<script lang="ts">
	let { diff }: { diff: string } = $props();

	const lines = $derived(diff.split('\n'));
</script>

<div class="max-h-96 overflow-auto p-2 font-mono text-xs" aria-label="File diff">
	{#each lines as line, index (index)}
		<div
			class="min-w-max {line.startsWith('+')
				? 'bg-green-50 text-green-800'
				: line.startsWith('-')
					? 'bg-red-50 text-red-800'
					: 'text-gray-600'}"
		>
			<code class="block whitespace-pre">{line || ' '}</code>
		</div>
	{/each}
</div>
