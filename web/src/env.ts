import { defineEnvVars } from '@sveltejs/kit/hooks';
import * as v from 'valibot';

const optionalText = v.optional(v.pipe(v.string(), v.trim()));

const variables = defineEnvVars({
	HUB_BASE_URL: {
		schema: v.pipe(v.string(), v.url())
	},
	HUB_AUTH_TOKEN: {
		schema: optionalText
	},
	CLOUDFLARE_ACCESS_TEAM_DOMAIN: {
		schema: optionalText
	},
	CLOUDFLARE_ACCESS_AUD: {
		schema: optionalText
	}
});

export { variables };
