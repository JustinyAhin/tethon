import { HUB_AUTH_TOKEN } from '$app/env/private';
import createClient from 'openapi-fetch';

import type { paths, components } from '@tethon/hub-sdk/hub-openapi';
import type { HostSummary, SessionSummary } from '$lib/types';
import type { CodexRespondRequest } from '@tethon/hub-sdk/protocol';

type InputRequest = components['schemas']['InputRequest'];
type HubErrorBody = components['schemas']['HubStatusError'];
type CreateSessionRequest = components['schemas']['CreateSessionRequest'];
type ResolveApprovalRequest = components['schemas']['ResolveApprovalRequest'];
type RepoRefsResponse = components['schemas']['RepoRefsResponse'];

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

const listRepoRefs = async ({
	baseUrl,
	hostId,
	repoId
}: {
	baseUrl: string;
	hostId: string;
	repoId: string;
}): Promise<RepoRefsResponse> => {
	const data = unwrap(
		await createHubClient(baseUrl).GET('/repos/{host_id}/{repo_id}/refs', {
			params: { path: { host_id: hostId, repo_id: repoId } }
		})
	);
	return { branches: data.branches ?? [], tags: data.tags ?? [] };
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

const sendInput = async ({
	baseUrl,
	id,
	text,
	...options
}: { baseUrl: string; id: string } & InputRequest) => {
	const path = sessionPath(id);
	return unwrap(
		await createHubClient(baseUrl).POST('/sessions/{host_id}/{local_id}/input', {
			params: { path },
			body: { text, ...options }
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

const cancelSession = async ({
	baseUrl,
	id,
	turn_id
}: {
	baseUrl: string;
	id: string;
	turn_id?: string;
}) => {
	const path = sessionPath(id);
	return unwrap(
		await createHubClient(baseUrl).POST('/sessions/{host_id}/{local_id}/cancel', {
			params: { path },
			body: turn_id ? { turn_id } : {}
		})
	);
};

const listAgentModels = async ({
	baseUrl,
	hostId,
	agent
}: {
	baseUrl: string;
	hostId: string;
	agent: string;
}) =>
	unwrap(
		await createHubClient(baseUrl).GET('/hosts/{host_id}/agents/{agent}/models', {
			params: { path: { host_id: hostId, agent } }
		})
	);

const listCodexConversations = async ({
	baseUrl,
	hostId,
	cursor,
	search
}: {
	baseUrl: string;
	hostId: string;
	cursor?: string;
	search?: string;
}) =>
	unwrap(
		await createHubClient(baseUrl).GET('/hosts/{host_id}/agents/codex-cli/conversations', {
			params: { path: { host_id: hostId }, query: { cursor, search } }
		})
	);

const getCodexSession = async ({ baseUrl, id }: { baseUrl: string; id: string }) =>
	unwrap(
		await createHubClient(baseUrl).GET('/sessions/{host_id}/{local_id}/native', {
			params: { path: sessionPath(id) }
		})
	);

const getCodexSettings = async ({
	baseUrl,
	hostId,
	cwd
}: {
	baseUrl: string;
	hostId: string;
	cwd?: string;
}) =>
	unwrap(
		await createHubClient(baseUrl).GET('/hosts/{host_id}/agents/codex-cli/settings', {
			params: { path: { host_id: hostId }, query: { cwd } }
		})
	);

const getCodexHistory = async ({
	baseUrl,
	id,
	cursor
}: {
	baseUrl: string;
	id: string;
	cursor?: string;
}) =>
	unwrap(
		await createHubClient(baseUrl).GET('/sessions/{host_id}/{local_id}/history', {
			params: { path: sessionPath(id), query: { cursor, limit: 20 } }
		})
	);

const respondCodexRequest = async ({
	baseUrl,
	id,
	body
}: {
	baseUrl: string;
	id: string;
	body: CodexRespondRequest;
}) =>
	unwrap(
		await createHubClient(baseUrl).POST('/sessions/{host_id}/{local_id}/requests/respond', {
			params: { path: sessionPath(id) },
			body
		})
	);

type NativeAction = 'archive' | 'unarchive' | 'delete' | 'cleanup';

const changeCodexConversation = async ({
	baseUrl,
	id,
	action
}: {
	baseUrl: string;
	id: string;
	action: NativeAction;
}) => {
	const client = createHubClient(baseUrl);
	const params = { path: sessionPath(id) };

	switch (action) {
		case 'archive':
			return unwrap(await client.POST('/sessions/{host_id}/{local_id}/archive', { params }));
		case 'unarchive':
			return unwrap(await client.POST('/sessions/{host_id}/{local_id}/unarchive', { params }));
		case 'delete':
			return unwrap(await client.DELETE('/sessions/{host_id}/{local_id}/conversation', { params }));
		case 'cleanup':
			return unwrap(
				await client.POST('/sessions/{host_id}/{local_id}/workspace/cleanup', { params })
			);
	}
};

const deleteSession = async ({ baseUrl, id }: { baseUrl: string; id: string }) => {
	const path = sessionPath(id);
	const result = await createHubClient(baseUrl).DELETE('/sessions/{host_id}/{local_id}', {
		params: { path }
	});
	if (result.error) {
		throw new HubAPIError({
			status: result.response.status,
			message: errorMessage({ error: result.error, status: result.response.status })
		});
	}
};

// Open the hub SSE stream and return the raw upstream Response so callers can
// pass the body through verbatim. Consuming the body in JS to re-emit it (the
// old streamSessionEvents generator) fails on Cloudflare Workers with
// "Network connection lost"; a verbatim pass-through is the supported pattern.
const openSessionStream = async ({
	baseUrl,
	id,
	from = 0,
	signal
}: {
	baseUrl: string;
	id: string;
	from?: number;
	signal?: AbortSignal;
}): Promise<Response> => {
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
	return res;
};

export {
	HubAPIError,
	cancelSession,
	createSession,
	deleteSession,
	listHosts,
	listRepoRefs,
	listSessions,
	listAgentModels,
	listCodexConversations,
	getCodexSession,
	getCodexSettings,
	getCodexHistory,
	respondCodexRequest,
	changeCodexConversation,
	openSessionStream,
	resolveApproval,
	sendInput
};
export type { CreateSessionRequest, RepoRefsResponse };
