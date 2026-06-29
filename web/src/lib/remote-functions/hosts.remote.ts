import { query } from '$app/server';
import { error } from '@sveltejs/kit';
import type { HostSummary } from '$lib/types';
import { HubAPIError, listHosts } from '$lib/server/hub-client';
import { hubBase } from './hub';

const getHosts = query(async (): Promise<HostSummary[]> => {
	try {
		return await listHosts({ baseUrl: hubBase() });
	} catch (err) {
		if (err instanceof HubAPIError) error(err.status, err.message);
		throw err;
	}
});

export { getHosts };
