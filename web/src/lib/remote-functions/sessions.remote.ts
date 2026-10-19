import { command, form, query } from '$app/server';
import * as v from 'valibot';

import type { SessionSummary } from '$lib/types';

import {
	HubAPIError,
	cancelSession as cancelHubSession,
	createSession as createHubSession,
	deleteSession as deleteHubSession,
	listSessions,
	resolveApproval as resolveHubApproval,
	sendInput as sendHubInput,
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
		permission_mode: optionalText,
		requested_model: optionalText,
		request_id: optionalText,
		reasoning_effort: optionalText,
		codex_permission_profile: optionalText,
		workspace_mode: v.optional(v.picklist(['', 'existing', 'isolated']), ''),
		native_thread_id: optionalText,
		prompt: optionalText
	}),
	v.forward(
		v.check(
			(data) => data.agent !== 'codex-cli' || v.is(v.pipe(v.string(), v.uuid()), data.request_id),
			'Refresh the form and try again.'
		),
		['request_id']
	),
	v.forward(
		v.check(
			(d) => Boolean(d.repo_id || d.repo || (d.agent === 'codex-cli' && d.native_thread_id)),
			'Choose a repo or enter a repo path.'
		),
		['repo_id']
	),
	v.forward(
		v.check(
			(data) => data.agent !== 'codex-cli' || Boolean(data.native_thread_id || data.prompt),
			'Enter a message to start a new conversation.'
		),
		['prompt']
	),
	v.forward(
		v.check(
			(data) =>
				!(
					data.agent === 'codex-cli' &&
					data.native_thread_id &&
					(data.requested_model || data.reasoning_effort || data.codex_permission_profile)
				) || Boolean(data.prompt),
			'Enter a message to apply these settings on the next turn.'
		),
		['prompt']
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
	if (data.permission_mode) body.permission_mode = data.permission_mode;
	if (data.prompt) body.prompt = data.prompt;
	if (data.requested_model) body.requested_model = data.requested_model;
	if (data.request_id) body.request_id = data.request_id;
	if (data.reasoning_effort) body.reasoning_effort = data.reasoning_effort;
	if (data.codex_permission_profile) body.codex_permission_profile = data.codex_permission_profile;
	if (data.workspace_mode) body.workspace_mode = data.workspace_mode;
	if (data.native_thread_id) body.native_thread_id = data.native_thread_id;

	try {
		const out = await createHubSession({ baseUrl: hubBase(), body });
		return { ok: true, id: out.id, status: out.status };
	} catch (err) {
		if (err instanceof HubAPIError) return { ok: false, error: err.message };
		throw err;
	}
});

const SendInputSchema = v.object({
	id: v.pipe(v.string(), v.nonEmpty()),
	text: v.pipe(v.string(), v.trim(), v.nonEmpty('Message is empty.')),
	mode: v.optional(v.picklist(['start', 'steer', 'queue'])),
	turn_id: v.optional(v.string()),
	request_id: v.optional(v.string()),
	requested_model: v.optional(v.string()),
	reasoning_effort: v.optional(v.string()),
	codex_permission_profile: v.optional(v.string())
});

type SendResult = { ok: true } | { ok: false; error: string };

const sendInput = command(SendInputSchema, async (data): Promise<SendResult> => {
	try {
		await sendHubInput({ baseUrl: hubBase(), ...data });
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
	id: v.pipe(v.string(), v.nonEmpty()),
	turn_id: v.optional(v.string())
});

const cancelSession = command(CancelSessionSchema, async ({ id, turn_id }): Promise<SendResult> => {
	try {
		await cancelHubSession({ baseUrl: hubBase(), id, turn_id });
		return { ok: true };
	} catch (err) {
		if (err instanceof HubAPIError) return { ok: false, error: err.message };
		throw err;
	}
});

const DeleteSessionSchema = v.object({
	id: v.pipe(v.string(), v.nonEmpty())
});

const deleteSession = command(DeleteSessionSchema, async ({ id }): Promise<SendResult> => {
	try {
		await deleteHubSession({ baseUrl: hubBase(), id });
		return { ok: true };
	} catch (err) {
		if (err instanceof HubAPIError) return { ok: false, error: err.message };
		throw err;
	}
});

export { getSessions, createSession, sendInput, resolveApproval, cancelSession, deleteSession };
