import { fetch as expoFetch } from 'expo/fetch';

import { HubAuthError, HubRequestError } from '@/lib/hub/client';
import { getCredentials } from '@/lib/hub/credentials';

type OpenSessionStreamArgs = {
  hostId: string;
  localId: string;
  from: number;
  signal: AbortSignal;
};

export const openSessionStream = async ({
  hostId,
  localId,
  from,
  signal,
}: OpenSessionStreamArgs): Promise<Response> => {
  const credentials = getCredentials();

  if (credentials === null) {
    throw new HubRequestError({ message: 'Missing hub credentials' });
  }

  const path =
    `/sessions/${encodeURIComponent(hostId)}` +
    `/${encodeURIComponent(localId)}/stream?from=${from}`;
  const url = new URL(path, `${credentials.url}/`);
  const headers = new Headers({ accept: 'text/event-stream' });

  if (credentials.token.length > 0) {
    headers.set('Authorization', `Bearer ${credentials.token}`);
  }

  // Streaming bypasses hubFetch because hubFetch installs a 10s timeout and
  // replaces the caller's abort signal, both of which break a long-lived SSE body.
  const response = await expoFetch(url, { headers, signal });

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
};
