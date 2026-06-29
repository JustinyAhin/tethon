import { HubAPIError, openSessionStream } from '$lib/server/hub-client';
import { hubBase } from '$lib/remote-functions/hub';

// Same-origin SSE relay. Cloudflare Workers cannot reliably read the hub stream
// in JS and re-emit it (that throws "Network connection lost"); instead we pass
// the upstream body through verbatim so the edge proxies it natively. The token
// stays server-side here. The browser consumes this with a fetch + reader loop.
export const GET = async ({ params, url, request }) => {
	const id = `${params.host}/${params.local}`;
	const fromRaw = Number(url.searchParams.get('from'));
	const from = Number.isFinite(fromRaw) && fromRaw > 0 ? Math.floor(fromRaw) : 0;

	try {
		const upstream = await openSessionStream({
			baseUrl: hubBase(),
			id,
			from,
			signal: request.signal
		});
		return new Response(upstream.body, {
			headers: {
				'content-type': 'text/event-stream',
				'cache-control': 'no-cache',
				connection: 'keep-alive'
			}
		});
	} catch (err) {
		if (err instanceof HubAPIError) {
			return new Response(err.message, { status: err.status });
		}
		throw err;
	}
};
