import { query } from '$app/server';
import { error } from '@sveltejs/kit';
import type { HostSummary } from '$lib/types';
import { hubBase, hubErrorMessage } from './hub';

const getHosts = query(async (): Promise<HostSummary[]> => {
	const base = hubBase();
	const res = await fetch(`${base}/hosts`);
	if (!res.ok) error(res.status, await hubErrorMessage(res));
	const data = (await res.json()) as { hosts?: HostSummary[] };
	return data.hosts ?? [];
});

export { getHosts };
