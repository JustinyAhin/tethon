import { getCredentials, type HubCredentials } from '@/lib/hub/credentials';
import type {
  InputRequest,
  ListHostsResponse,
  ListSessionsResponse,
} from '@/lib/hub/types';

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

  constructor({
    message,
    status,
  }: {
    message: string;
    status?: number;
  }) {
    super(message);
    this.name = 'HubRequestError';
    this.status = status;
  }
}

export type HubFetchArgs = {
  path: string;
  init?: RequestInit;
  credentials?: HubCredentials;
};

export const hubFetch = async ({
  path,
  init,
  credentials,
}: HubFetchArgs): Promise<Response> => {
  const activeCredentials = credentials ?? getCredentials();

  if (activeCredentials === null) {
    throw new HubRequestError({ message: 'Missing hub credentials' });
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => {
    controller.abort();
  }, requestTimeoutMs);
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
      throw new HubRequestError({
        message: `Hub returned HTTP ${response.status}`,
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

export const sessionPath = (id: string): { hostId: string; localId: string } => {
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

export const cancelSession = async ({
  hostId,
  localId,
}: {
  hostId: string;
  localId: string;
}): Promise<void> => {
  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/cancel`,
    init: { method: 'POST' },
  });
};

export const sendInput = async ({
  hostId,
  localId,
  text,
}: {
  hostId: string;
  localId: string;
  text: string;
}): Promise<void> => {
  const body = { text } satisfies InputRequest;

  await hubFetch({
    path: `/sessions/${encodeURIComponent(hostId)}/${encodeURIComponent(localId)}/input`,
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
