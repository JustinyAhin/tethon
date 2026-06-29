<script lang="ts" module>
	import MarkdownIt from 'markdown-it';

	const parser = new MarkdownIt({
		html: false,
		linkify: true,
		typographer: false
	});
</script>

<script lang="ts">
	import { watch } from 'runed';

	let { source }: { source: string } = $props();

	let html = $state('');
	let renderError = $state('');

	watch(
		() => source,
		(markdownSource) => {
			renderError = '';

			try {
				html = parser.render(markdownSource);
			} catch (err) {
				html = '';
				renderError = err instanceof Error ? err.message : 'Could not render markdown.';
			}
		}
	);
</script>

{#if html}
	<div class="markdown-output text-sm leading-relaxed">
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- markdown-it escapes raw HTML when html=false -->
		{@html html}
	</div>
{:else}
	<div class="whitespace-pre-wrap text-sm leading-relaxed">
		{source}
	</div>
{/if}

{#if renderError}
	<p class="mt-2 text-xs text-red-600">{renderError}</p>
{/if}

<style>
	.markdown-output :global(:where(p, ul, ol, pre, blockquote, table)) {
		margin-block: 0.75rem;
	}

	.markdown-output :global(:where(p, ul, ol, pre, blockquote, table):first-child) {
		margin-top: 0;
	}

	.markdown-output :global(:where(p, ul, ol, pre, blockquote, table):last-child) {
		margin-bottom: 0;
	}

	.markdown-output :global(:where(h1, h2, h3, h4, h5, h6)) {
		margin-block: 1rem 0.5rem;
		font-weight: 650;
		line-height: 1.25;
	}

	.markdown-output :global(h1) {
		font-size: 1.25rem;
	}

	.markdown-output :global(h2) {
		font-size: 1.125rem;
	}

	.markdown-output :global(h3) {
		font-size: 1rem;
	}

	.markdown-output :global(ul) {
		list-style: disc;
		padding-left: 1.25rem;
	}

	.markdown-output :global(ol) {
		list-style: decimal;
		padding-left: 1.25rem;
	}

	.markdown-output :global(li) {
		margin-block: 0.25rem;
	}

	.markdown-output :global(code) {
		border-radius: 0.25rem;
		background: rgb(243 244 246);
		padding: 0.125rem 0.25rem;
		font-size: 0.875em;
	}

	.markdown-output :global(pre) {
		overflow-x: auto;
		border-radius: 0.375rem;
		background: rgb(243 244 246);
		padding: 0.75rem;
	}

	.markdown-output :global(pre code) {
		background: transparent;
		padding: 0;
	}

	.markdown-output :global(blockquote) {
		border-left: 3px solid rgb(209 213 219);
		padding-left: 0.75rem;
		color: rgb(75 85 99);
	}
</style>
