import { query } from '$app/server';
import * as v from 'valibot';
import { HubAPIError, listRepoRefs } from '$lib/server/hub-client';
import { hubBase } from './hub';

type RefsResult =
	{ ok: true; branches: string[]; tags: string[] } | { ok: false; error: string; status: number };

const loadError = (err: unknown): RefsResult => {
	if (err instanceof HubAPIError) {
		return { ok: false, error: err.message || `Hub error ${err.status}`, status: err.status };
	}
	if (err instanceof Error && err.message) {
		return { ok: false, error: err.message, status: 0 };
	}
	return { ok: false, error: 'Could not load refs.', status: 0 };
};

const RepoRefsArgs = v.object({
	host_id: v.pipe(v.string(), v.trim(), v.nonEmpty()),
	repo_id: v.pipe(v.string(), v.trim(), v.nonEmpty())
});

const getRepoRefs = query(RepoRefsArgs, async ({ host_id, repo_id }): Promise<RefsResult> => {
	try {
		const refs = await listRepoRefs({ baseUrl: hubBase(), hostId: host_id, repoId: repo_id });
		return { ok: true, branches: refs.branches ?? [], tags: refs.tags ?? [] };
	} catch (err) {
		return loadError(err);
	}
});

export { getRepoRefs };
