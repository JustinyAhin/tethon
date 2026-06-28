import { form } from '$app/server';
import * as v from 'valibot';
import type { CreateSessionResponse } from '$lib/types';
import { hubBase, hubErrorMessage } from './hub';

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
	const base = hubBase();

	const body: Record<string, string> = { host_id: data.host_id };
	if (data.repo_id) body.repo_id = data.repo_id;
	else if (data.repo) body.repo = data.repo;
	for (const k of ['cwd', 'branch', 'base_ref', 'agent', 'prompt'] as const) {
		if (data[k]) body[k] = data[k];
	}

	const res = await fetch(`${base}/sessions`, {
		method: 'POST',
		headers: { 'content-type': 'application/json' },
		body: JSON.stringify(body)
	});
	if (!res.ok) return { ok: false, error: await hubErrorMessage(res) };

	const out = (await res.json()) as CreateSessionResponse;
	return { ok: true, id: out.id, status: out.status };
});

export { createSession };
