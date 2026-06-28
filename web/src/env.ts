import { defineEnvVars } from '@sveltejs/kit/hooks';
import * as v from 'valibot';

const variables = defineEnvVars({
	HUB_BASE_URL: {
		schema: v.pipe(v.string(), v.url())
	}
});

export { variables };
