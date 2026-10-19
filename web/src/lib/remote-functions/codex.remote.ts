import { query, command } from '$app/server';
import * as v from 'valibot';
import {
	HubAPIError,
	listAgentModels,
	listCodexConversations,
	getCodexSession,
	getCodexSettings,
	getCodexHistory,
	respondCodexRequest,
	changeCodexConversation
} from '$lib/server/hub-client';
import { hubBase } from './hub';

const text = v.pipe(v.string(), v.trim(), v.nonEmpty());

const read = async <T>(operation: () => Promise<T>) => {
	try {
		return { ok: true as const, value: await operation() };
	} catch (error) {
		return {
			ok: false as const,
			error: error instanceof Error ? error.message : 'The host could not complete this request.',
			status: error instanceof HubAPIError ? error.status : 0
		};
	}
};

const getModels = query(v.object({ host_id: text, agent: text }), ({ host_id, agent }) =>
	read(() => listAgentModels({ baseUrl: hubBase(), hostId: host_id, agent }))
);

const getSettings = query(
	v.object({ host_id: text, cwd: v.optional(v.string()) }),
	({ host_id, cwd }) => read(() => getCodexSettings({ baseUrl: hubBase(), hostId: host_id, cwd }))
);

const getConversations = query(
	v.object({ host_id: text, cursor: v.optional(v.string()), search: v.optional(v.string()) }),
	({ host_id, cursor, search }) =>
		read(() => listCodexConversations({ baseUrl: hubBase(), hostId: host_id, cursor, search }))
);

const getNativeSession = query(v.object({ id: text }), ({ id }) =>
	read(() => getCodexSession({ baseUrl: hubBase(), id }))
);

const getNativeHistory = query(
	v.object({ id: text, cursor: v.optional(v.string()) }),
	({ id, cursor }) => read(() => getCodexHistory({ baseUrl: hubBase(), id, cursor }))
);

const respondNativeRequest = command(
	v.object({ id: text, request_key: text, connection_id: text, result: v.unknown() }),
	({ id, ...body }) => read(() => respondCodexRequest({ baseUrl: hubBase(), id, body }))
);

const changeNativeConversation = command(
	v.object({ id: text, action: v.picklist(['archive', 'unarchive', 'delete', 'cleanup']) }),
	({ id, action }) => read(() => changeCodexConversation({ baseUrl: hubBase(), id, action }))
);

export {
	getModels,
	getSettings,
	getConversations,
	getNativeSession,
	getNativeHistory,
	respondNativeRequest,
	changeNativeConversation
};
