<script lang="ts" module>
	type MarkdownSatteri = typeof import('@astrojs/markdown-satteri');
	type MarkdownProcessor = Awaited<ReturnType<MarkdownSatteri['createSatteriMarkdownProcessor']>>;

	let processor: Promise<MarkdownProcessor> | undefined;

	const getProcessor = async () => {
		processor ??= import('@astrojs/markdown-satteri').then((mod) =>
			mod.createSatteriMarkdownProcessor({
				syntaxHighlight: false,
				smartypants: false,
				features: { frontmatter: false }
			})
		);
		return processor;
	};

	const escapeRawHTML = (markdown: string): string =>
		markdown.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
</script>

<script lang="ts">
	import { watch } from 'runed';

	let { source }: { source: string } = $props();

	let html = $state('');
	let renderError = $state('');
	let version = 0;

	watch(
		() => source,
		(markdown) => {
			const current = ++version;
			renderError = '';

			getProcessor()
				.then((renderer) => renderer.render(escapeRawHTML(markdown)))
				.then((result) => {
					if (current !== version) return;
					html = result.code;
				})
				.catch((err: unknown) => {
					if (current !== version) return;
					html = '';
					renderError = err instanceof Error ? err.message : 'Could not render markdown.';
				});
		}
	);
</script>

{#if html}
	<div class="markdown-output text-sm leading-relaxed">
		<!-- eslint-disable-next-line svelte/no-at-html-tags -- raw HTML is escaped before markdown rendering -->
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
