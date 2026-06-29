import { HUB_AUTH_TOKEN } from '$app/env/private';
import createClient from 'openapi-fetch';
import type { paths, components } from '$lib/generated/hub-openapi';
import type { HostSummary, SessionEvent, SessionSummary } from '$lib/types';

type HubErrorBody = components['schemas']['HubStatusError'];
type CreateSessionRequest = components['schemas']['CreateSessionRequest'];
type ResolveApprovalRequest = components['schemas']['ResolveApprovalRequest'];

class HubAPIError extends Error {
	status: number;

	constructor({ status, message }: { status: number; message: string }) {
		super(message);
		this.name = 'HubAPIError';
		this.status = status;
	}
}

const hubHeaders = (headers?: HeadersInit): Headers => {
	const out = new Headers(headers);
	if (HUB_AUTH_TOKEN) out.set('Authorization', `Bearer ${HUB_AUTH_TOKEN}`);
	return out;
};

const createHubClient = (baseUrl: string) =>
	createClient<paths>({ baseUrl: baseUrl.replace(/\/$/, ''), headers: hubHeaders() });

const errorMessage = ({ error, status }: { error: unknown; status: number }): string => {
	const body = error as Partial<HubErrorBody> | undefined;
	if (typeof body?.error === 'string' && body.error) return body.error;
	return `Hub error ${status}`;
};

const unwrap = <T>(result: { data?: T; error?: unknown; response: Response }): T => {
	if (result.error) {
		throw new HubAPIError({
			status: result.response.status,
			message: errorMessage({ error: result.error, status: result.response.status })
		});
	}
	if (result.data === undefined) {
		throw new HubAPIError({
			status: result.response.status,
			message: `Hub error ${result.response.status}`
		});
	}
	return result.data;
};

const sessionPath = (id: string): { host_id: string; local_id: string } => {
	const slash = id.indexOf('/');
	if (slash <= 0 || slash === id.length - 1) {
		throw new HubAPIError({ status: 400, message: 'session id must be host/local' });
	}
	return { host_id: id.slice(0, slash), local_id: id.slice(slash + 1) };
};

const normalizeHost = (host: components['schemas']['HostSummary']): HostSummary => ({
	...host,
	agents: host.agents ?? [],
	repo_roots: host.repo_roots ?? [],
	repos: host.repos ?? []
});

const normalizeSession = (session: components['schemas']['SessionSummary']): SessionSummary => ({
	...session,
	host_id: session.host_id ?? ''
});

const listHosts = async ({ baseUrl }: { baseUrl: string }): Promise<HostSummary[]> => {
	const data = unwrap(await createHubClient(baseUrl).GET('/hosts'));
	return (data.hosts ?? []).map(normalizeHost);
};

const listSessions = async ({ baseUrl }: { baseUrl: string }): Promise<SessionSummary[]> => {
	const data = unwrap(await createHubClient(baseUrl).GET('/sessions'));
	return (data.sessions ?? []).map(normalizeSession);
};

const createSession = async ({
	baseUrl,
	body
}: {
	baseUrl: string;
	body: CreateSessionRequest;
}) => {
	return unwrap(await createHubClient(baseUrl).POST('/sessions', { body }));
};

const sendInput = async ({ baseUrl, id, text }: { baseUrl: string; id: string; text: string }) => {
	const path = sessionPath(id);
	return unwrap(
		await createHubClient(baseUrl).POST('/sessions/{host_id}/{local_id}/input', {
			params: { path },
			body: { text }
		})
	);
};

const resolveApproval = async ({
	baseUrl,
	id,
	requestID,
	body
}: {
	baseUrl: string;
	id: string;
	requestID: string;
	body: ResolveApprovalRequest;
}) => {
	const path = { ...sessionPath(id), request_id: requestID };
	return unwrap(
		await createHubClient(baseUrl).POST('/sessions/{host_id}/{local_id}/approvals/{request_id}', {
			params: { path },
			body
		})
	);
};

const cancelSession = async ({ baseUrl, id }: { baseUrl: string; id: string }) => {
	const path = sessionPath(id);
	return unwrap(
		await createHubClient(baseUrl).POST('/sessions/{host_id}/{local_id}/cancel', {
			params: { path }
		})
	);
};

const parseFrame = (frame: string): SessionEvent | null => {
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

const streamSessionEvents = async function* ({
	baseUrl,
	id,
	from = 0,
	signal
}: {
	baseUrl: string;
	id: string;
	from?: number;
	signal?: AbortSignal;
}): AsyncGenerator<SessionEvent> {
	const path = sessionPath(id);
	const params = new URLSearchParams({ from: String(from) });
	const url = `${baseUrl.replace(/\/$/, '')}/sessions/${path.host_id}/${path.local_id}/stream?${params}`;
	const res = await fetch(url, {
		signal,
		headers: hubHeaders({ accept: 'text/event-stream' })
	});
	if (!res.ok || !res.body) {
		let message = `Hub error ${res.status}`;
		try {
			const body = (await res.json()) as Partial<HubErrorBody>;
			if (typeof body.error === 'string' && body.error) message = body.error;
		} catch {
			// non-JSON body
		}
		throw new HubAPIError({ status: res.status, message });
	}

	const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
	let buffer = '';
	for (;;) {
		const { done, value } = await reader.read();
		if (done) break;
		buffer += value;
		let sep: number;
		while ((sep = buffer.indexOf('\n\n')) !== -1) {
			const event = parseFrame(buffer.slice(0, sep));
			buffer = buffer.slice(sep + 2);
			if (!event) continue;
			yield event;
			if (event.type === 'done') return;
		}
	}
};

export {
	HubAPIError,
	cancelSession,
	createSession,
	listHosts,
	listSessions,
	resolveApproval,
	sendInput,
	streamSessionEvents
};
export type { CreateSessionRequest };
