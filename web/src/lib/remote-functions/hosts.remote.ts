import { query } from '$app/server';
import type { HostSummary } from '$lib/types';
import { HubAPIError, listHosts } from '$lib/server/hub-client';
import { hubBase } from './hub';

type HostsResult =
	{ ok: true; hosts: HostSummary[] } | { ok: false; error: string; status: number };

const loadError = (err: unknown): HostsResult => {
	if (err instanceof HubAPIError) {
		return { ok: false, error: err.message || `Hub error ${err.status}`, status: err.status };
	}
	if (err instanceof Error && err.message) {
		return { ok: false, error: err.message, status: 0 };
	}
	return { ok: false, error: 'Could not load hosts.', status: 0 };
};

const getHosts = query(async (): Promise<HostsResult> => {
	try {
		return { ok: true, hosts: await listHosts({ baseUrl: hubBase() }) };
	} catch (err) {
		return loadError(err);
	}
});

export { getHosts };
