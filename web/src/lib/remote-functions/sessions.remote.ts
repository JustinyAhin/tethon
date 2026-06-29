import { form, query } from '$app/server';
import { error } from '@sveltejs/kit';
import * as v from 'valibot';
import type { CreateSessionResponse, SessionEvent } from '$lib/types';
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

const streamSession = query.live(v.string(), async function* (id) {
	const base = hubBase();
	const controller = new AbortController();
	try {
		const res = await fetch(`${base}/sessions/${id}/stream?from=0`, {
			signal: controller.signal,
			headers: { accept: 'text/event-stream' }
		});
		if (!res.ok || !res.body) error(res.status, await hubErrorMessage(res));

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
	} finally {
		controller.abort();
	}
});

export { createSession, streamSession };
