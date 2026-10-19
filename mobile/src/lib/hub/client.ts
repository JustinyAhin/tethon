import { getCredentials, type HubCredentials } from '@/lib/hub/credentials';
import type {
  ApprovalAction,
  CreateSessionRequest,
  CreateSessionResponse,
  InputRequest,
  ListHostsResponse,
  ListSessionsResponse,
  RepoRefsResponse,
  RegisterDeviceRequest,
  RegisterDeviceResponse,
  ResolveApprovalRequest,
  CodexSessionResponse,
  CodexHistoryResponse,
  CodexConversation,
  CodexConversationsResponse,
  CodexRespondRequest,
  ModelCatalog,
  CodexSettingsResponse,
} from '@/lib/hub/types';

type NativeConversation = CodexConversation;
type NativeSessionAction = 'archive' | 'unarchive' | 'delete' | 'cleanup';

const requestTimeoutMs = 10_000;

export class HubAuthError extends Error {
  status = 401;

  constructor(message = 'Token rejected by hub') {
    super(message);
    this.name = 'HubAuthError';
  }
}

export class HubRequestError extends Error {
  status?: number;

  constructor({ message, status }: { message: string; status?: number }) {
    super(message);
    this.name = 'HubRequestError';
    this.status = status;
  }
}

export type HubFetchArgs = {
  path: string;
  init?: RequestInit;
  credentials?: HubCredentials;
  timeoutMs?: number;
};

export const hubFetch = async ({
  path,
  init,
  credentials,
  timeoutMs = requestTimeoutMs,
}: HubFetchArgs): Promise<Response> => {
  const activeCredentials = credentials ?? getCredentials();

  if (activeCredentials === null) {
    throw new HubRequestError({ message: 'Missing hub credentials' });
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => {
    controller.abort();
  }, timeoutMs);
  const headers = new Headers(init?.headers);

  if (activeCredentials.token.length > 0) {
    headers.set('Authorization', `Bearer ${activeCredentials.token}`);
  } else {
    headers.delete('Authorization');
  }

  try {
    const response = await fetch(new URL(path, `${activeCredentials.url}/`), {
      ...init,
      headers,
      signal: controller.signal,
    });

    if (response.status === 401) {
      throw new HubAuthError();
    }

    if (!response.ok) {
      let message = `Hub returned HTTP ${response.status}`;

      try {
        const body: unknown = await response.json();

        if (
          typeof body === 'object' &&
          body !== null &&
          'error' in body &&
          typeof body.error === 'string'
        ) {
          message = body.error.slice(0, 800);
        }
      } catch {
        /* Keep the HTTP error when there is no JSON body. */
      }

      throw new HubRequestError({
        message,
        status: response.status,
      });
    }

    return response;
  } catch (error) {
    if (error instanceof HubAuthError || error instanceof HubRequestError) {
      throw error;
    }

    throw new HubRequestError({ message: 'Could not reach hub' });
  } finally {
    clearTimeout(timeout);
  }
};

export const fetchHosts = async (
  credentials?: HubCredentials,
): Promise<ListHostsResponse> => {
  const response = await hubFetch({
    path: '/hosts',
    init: { method: 'GET' },
    credentials,
  });

  return (await response.json()) as ListHostsResponse;
};

export const fetchRepoRefs = async ({
  hostId,
  repoId,
}: {
  hostId: string;
  repoId: string;
}): Promise<RepoRefsResponse> => {
  const response = await hubFetch({
    path: `/repos/${encodeURIComponent(hostId)}/${encodeURIComponent(repoId)}/refs`,
    init: { method: 'GET' },
  });

  return (await response.json()) as RepoRefsResponse;
};

export const sessionPath = (
  id: string,
): { hostId: string; localId: string } => {
  const slash = id.indexOf('/');

  if (slash <= 0 || slash === id.length - 1) {
    throw new HubRequestError({
      message: 'Session id must be host/local',
      status: 400,
    });
  }

  return { hostId: id.slice(0, slash), localId: id.slice(slash + 1) };
};

export const fetchSessions = async (
  credentials?: HubCredentials,
): Promise<ListSessionsResponse> => {
  const response = await hubFetch({
    path: '/sessions',
    init: { method: 'GET' },
    credentials,
  });

  return (await response.json()) as ListSessionsResponse;
};

export const registerDevice = async (
  body: RegisterDeviceRequest,
): Promise<RegisterDeviceResponse> => {
  const response = await hubFetch({
    path: '/devices/register',
    init: {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  });

  return (await response.json()) as RegisterDeviceResponse;
};

export const unregisterDevice = async ({
  deviceId,
}: {
  deviceId: string;
}): Promise<void> => {
  await hubFetch({
    path: `/devices/${encodeURIComponent(deviceId)}`,
    init: { method: 'DELETE' },
  });
};

export const createSession = async (
  body: CreateSessionRequest,
): Promise<CreateSessionResponse> => {
  const response = await hubFetch({
    path: '/sessions',
    init: {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  });

  return (await response.json()) as CreateSessionResponse;
};

export const cancelSession = async ({
  hostId,
  localId,
  turnId,
}: {
  hostId: string;
  localId: string;
  turnId?: string;
}): Promise<void> => {
  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/cancel`,
    init: {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(turnId === undefined ? {} : { turn_id: turnId }),
    },
  });
};

export const sendInput = async ({
  hostId,
  localId,
  text,
  mode,
  turnId,
  requestId,
  requestedModel,
  reasoningEffort,
  permissionProfile,
}: {
  hostId: string;
  localId: string;
  text: string;
  mode?: 'start' | 'steer' | 'queue';
  turnId?: string;
  requestId?: string;
  requestedModel?: string;
  reasoningEffort?: string;
  permissionProfile?: string;
}): Promise<void> => {
  const body: InputRequest = {
    text,
    mode,
    turn_id: turnId,
    request_id: requestId,
    requested_model: requestedModel,
    reasoning_effort: reasoningEffort,
    codex_permission_profile: permissionProfile,
  };

  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/input`,
    init: {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  });
};

export const resolveApproval = async ({
  action,
  hostId,
  localId,
  requestId,
  text,
}: {
  action: ApprovalAction;
  hostId: string;
  localId: string;
  requestId: string;
  text?: string;
}): Promise<void> => {
  const body: ResolveApprovalRequest =
    action === 'reply' ? { action, text: text ?? '' } : { action };

  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/approvals/${encodeURIComponent(requestId)}`,
    init: {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  });
};

export const deleteSession = async ({
  hostId,
  localId,
}: {
  hostId: string;
  localId: string;
}): Promise<void> => {
  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}`,
    init: { method: 'DELETE' },
  });
};

const fetchModelCatalog = async ({
  hostId,
  agent,
}: {
  hostId: string;
  agent: string;
}): Promise<ModelCatalog> => {
  const response = await hubFetch({
    path: `/hosts/${encodeURIComponent(hostId)}/agents/${encodeURIComponent(agent)}/models`,
  });
  return (await response.json()) as ModelCatalog;
};

const fetchNativeSession = async ({
  hostId,
  localId,
}: {
  hostId: string;
  localId: string;
}): Promise<CodexSessionResponse> => {
  const response = await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/native`,
  });
  return (await response.json()) as CodexSessionResponse;
};

const respondToNativeRequest = async ({
  hostId,
  localId,
  requestKey,
  connectionId,
  result,
}: {
  hostId: string;
  localId: string;
  requestKey: string;
  connectionId: string;
  result: unknown;
}): Promise<void> => {
  const body: CodexRespondRequest = {
    request_key: requestKey,
    connection_id: connectionId,
    result,
  };

  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/requests/respond`,
    init: {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    },
  });
};

const fetchNativeHistory = async ({
  hostId,
  localId,
  cursor,
}: {
  hostId: string;
  localId: string;
  cursor?: string;
}): Promise<CodexHistoryResponse> => {
  const query = new URLSearchParams({ limit: '20' });

  if (cursor) {
    query.set('cursor', cursor);
  }

  const response = await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/history?${query}`,
  });

  return (await response.json()) as CodexHistoryResponse;
};

const fetchNativeConversations = async ({
  hostId,
  cursor,
  search,
}: {
  hostId: string;
  cursor?: string;
  search?: string;
}): Promise<CodexConversationsResponse> => {
  const query = new URLSearchParams();

  if (cursor) {
    query.set('cursor', cursor);
  }

  if (search) {
    query.set('search', search);
  }

  const response = await hubFetch({
    path: `/hosts/${encodeURIComponent(hostId)}/agents/codex-cli/conversations?${query}`,
  });

  return (await response.json()) as CodexConversationsResponse;
};

const fetchCodexSettings = async ({
  hostId,
  cwd,
}: {
  hostId: string;
  cwd?: string;
}): Promise<CodexSettingsResponse> => {
  const query = new URLSearchParams();

  if (cwd) {
    query.set('cwd', cwd);
  }

  const response = await hubFetch({
    path: `/hosts/${encodeURIComponent(hostId)}/agents/codex-cli/settings?${query}`,
    timeoutMs: 60_000,
  });

  return (await response.json()) as CodexSettingsResponse;
};

const changeNativeConversation = async ({
  hostId,
  localId,
  action,
}: {
  hostId: string;
  localId: string;
  action: NativeSessionAction;
}): Promise<void> => {
  const operation =
    action === 'delete'
      ? 'conversation'
      : action === 'cleanup'
        ? 'workspace/cleanup'
        : action;

  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/${operation}`,
    init: { method: action === 'delete' ? 'DELETE' : 'POST' },
  });
};

export {
  fetchModelCatalog,
  fetchNativeSession,
  respondToNativeRequest,
  fetchNativeHistory,
  fetchNativeConversations,
  fetchCodexSettings,
  changeNativeConversation,
  type NativeConversation,
  type NativeSessionAction,
};
