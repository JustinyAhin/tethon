import { CLOUDFLARE_ACCESS_AUD, CLOUDFLARE_ACCESS_TEAM_DOMAIN } from '$app/env/private';
import { error } from '@sveltejs/kit';
import { createRemoteJWKSet, jwtVerify } from 'jose';
import type { Handle } from '@sveltejs/kit';

const isLocalHostname = (hostname: string): boolean =>
	hostname === 'localhost' ||
	hostname === '127.0.0.1' ||
	hostname === '[::1]' ||
	hostname === '::1' ||
	hostname.endsWith('.localhost');

const teamDomain = (): string | null => {
	if (!CLOUDFLARE_ACCESS_TEAM_DOMAIN) return null;
	return CLOUDFLARE_ACCESS_TEAM_DOMAIN.replace(/\/$/, '');
};

const verifyAccessJWT = async (token: string): Promise<void> => {
	const domain = teamDomain();
	if (!domain || !CLOUDFLARE_ACCESS_AUD) {
		error(503, 'Cloudflare Access is not configured');
	}

	const jwks = createRemoteJWKSet(new URL(`${domain}/cdn-cgi/access/certs`));
	await jwtVerify(token, jwks, {
		issuer: domain,
		audience: CLOUDFLARE_ACCESS_AUD
	});
};

const handle: Handle = async ({ event, resolve }) => {
	if (isLocalHostname(event.url.hostname)) {
		return resolve(event);
	}

	const token = event.request.headers.get('cf-access-jwt-assertion');
	if (!token) {
		error(401, 'Missing Cloudflare Access JWT');
	}

	try {
		await verifyAccessJWT(token);
	} catch (err) {
		if (err && typeof err === 'object' && 'status' in err) throw err;
		error(403, 'Invalid Cloudflare Access JWT');
	}

	return resolve(event);
};

export { handle };
