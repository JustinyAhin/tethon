import { command, form, query } from '$app/server';
import { error } from '@sveltejs/kit';
import * as v from 'valibot';
import type { SessionSummary } from '$lib/types';
import {
	HubAPIError,
	cancelSession as cancelHubSession,
	createSession as createHubSession,
	listSessions,
	resolveApproval as resolveHubApproval,
	sendInput as sendHubInput,
	streamSessionEvents,
	type CreateSessionRequest
} from '$lib/server/hub-client';
import { hubBase } from './hub';

type SessionsResult =
	{ ok: true; sessions: SessionSummary[] } | { ok: false; error: string; status: number };

const loadError = (err: unknown): SessionsResult => {
	if (err instanceof HubAPIError) {
		return { ok: false, error: err.message || `Hub error ${err.status}`, status: err.status };
	}
	if (err instanceof Error && err.message) {
		return { ok: false, error: err.message, status: 0 };
	}
	return { ok: false, error: 'Could not load sessions.', status: 0 };
};

const getSessions = query(async (): Promise<SessionsResult> => {
	try {
		return { ok: true, sessions: await listSessions({ baseUrl: hubBase() }) };
	} catch (err) {
		return loadError(err);
	}
});

const optionalText = v.optional(v.pipe(v.string(), v.trim()), '');

const CreateSessionSchema = v.pipe(
	v.object({
		host_id: v.pipe(v.string(), v.trim(), v.nonEmpty('Select a host.')),
		repo_id: optionalText,
		repo: optionalText,
		cwd: optionalText,
		branch: optionalText,
		base_ref: optionalText,
		agent: optionalText,
		prompt: optionalText
	}),
	v.forward(
		v.check((d) => Boolean(d.repo_id || d.repo), 'Choose a repo or enter a repo path.'),
		['repo_id']
	)
);

type CreateResult = { ok: true; id: string; status: string } | { ok: false; error: string };

const createSession = form(CreateSessionSchema, async (data): Promise<CreateResult> => {
	const body: CreateSessionRequest = {
		host_id: data.host_id,
		cwd: data.cwd,
		branch: data.branch,
		agent: data.agent
	};
	if (data.repo_id) body.repo_id = data.repo_id;
	else if (data.repo) body.repo = data.repo;
	if (data.base_ref) body.base_ref = data.base_ref;
	if (data.prompt) body.prompt = data.prompt;

	try {
		const out = await createHubSession({ baseUrl: hubBase(), body });
		return { ok: true, id: out.id, status: out.status };
	} catch (err) {
		if (err instanceof HubAPIError) return { ok: false, error: err.message };
		throw err;
	}
});

const streamSession = query.live(v.string(), async function* (id) {
	const controller = new AbortController();
	try {
		for await (const event of streamSessionEvents({
			baseUrl: hubBase(),
			id,
			signal: controller.signal
		})) {
			yield event;
			if (event.type === 'done') return;
		}
	} catch (err) {
		if (err instanceof HubAPIError) error(err.status, err.message);
		throw err;
	} finally {
		controller.abort();
	}
});

const SendInputSchema = v.object({
	id: v.pipe(v.string(), v.nonEmpty()),
	text: v.pipe(v.string(), v.trim(), v.nonEmpty('Message is empty.'))
});

type SendResult = { ok: true } | { ok: false; error: string };

const sendInput = command(SendInputSchema, async ({ id, text }): Promise<SendResult> => {
	try {
		await sendHubInput({ baseUrl: hubBase(), id, text });
		return { ok: true };
	} catch (err) {
		if (err instanceof HubAPIError) return { ok: false, error: err.message };
		throw err;
	}
});

const ResolveApprovalSchema = v.pipe(
	v.object({
		id: v.pipe(v.string(), v.nonEmpty()),
		request_id: v.pipe(v.string(), v.nonEmpty()),
		action: v.picklist(['approve', 'deny', 'reply']),
		text: optionalText
	}),
	v.forward(
		v.check((d) => d.action !== 'reply' || Boolean(d.text), 'Reply text is required.'),
		['text']
	)
);

const resolveApproval = command(
	ResolveApprovalSchema,
	async ({ id, request_id, action, text }): Promise<SendResult> => {
		const body: { action: string; text?: string } = { action };
		if (action === 'reply') body.text = text;
		try {
			await resolveHubApproval({ baseUrl: hubBase(), id, requestID: request_id, body });
			return { ok: true };
		} catch (err) {
			if (err instanceof HubAPIError) return { ok: false, error: err.message };
			throw err;
		}
	}
);

const CancelSessionSchema = v.object({
	id: v.pipe(v.string(), v.nonEmpty())
});

const cancelSession = command(CancelSessionSchema, async ({ id }): Promise<SendResult> => {
	try {
		await cancelHubSession({ baseUrl: hubBase(), id });
		return { ok: true };
	} catch (err) {
		if (err instanceof HubAPIError) return { ok: false, error: err.message };
		throw err;
	}
});

export { getSessions, createSession, streamSession, sendInput, resolveApproval, cancelSession };
