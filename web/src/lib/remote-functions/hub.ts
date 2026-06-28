import { HUB_BASE_URL } from '$app/env/private';

const hubBase = (): string => HUB_BASE_URL.replace(/\/$/, '');

const hubErrorMessage = async (res: Response): Promise<string> => {
	try {
		const body = (await res.json()) as { error?: unknown };
		if (typeof body.error === 'string' && body.error) return body.error;
	} catch {
		// non-JSON body
	}
	return `Hub error ${res.status}`;
};

export { hubBase, hubErrorMessage };
