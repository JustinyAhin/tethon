import { getCredentials, type HubCredentials } from '@/lib/hub/credentials';
import type { ListHostsResponse } from '@/lib/hub/types';

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
