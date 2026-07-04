import * as SecureStore from 'expo-secure-store';

export type HubCredentials = {
  url: string;
  token: string;
};

const hubUrlKey = 'tethon.hub-url';
const hubTokenKey = 'tethon.hub-token';

let credentialsCache: HubCredentials | null = null;
let hubUrlCache: string | null = null;

export const normalizeHubUrl = (raw: string): string => {
  const trimmed = raw.trim();

  if (trimmed.length === 0) {
    throw new TypeError('Hub URL is required');
  }

  const withScheme = trimmed.includes('://') ? trimmed : `http://${trimmed}`;
  const url = new URL(withScheme);

  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    throw new TypeError('Hub URL must use http or https');
  }

  return url.toString().replace(/\/+$/, '');
};

export const loadCredentials = async (): Promise<HubCredentials | null> => {
  const [storedUrl, storedToken] = await Promise.all([
    SecureStore.getItemAsync(hubUrlKey),
    SecureStore.getItemAsync(hubTokenKey),
  ]);

  if (storedUrl === null) {
    credentialsCache = null;
    hubUrlCache = null;
    return null;
  }

  const normalizedUrl = normalizeHubUrl(storedUrl);
  hubUrlCache = normalizedUrl;

  if (storedToken === null) {
    credentialsCache = null;
    return null;
  }

  credentialsCache = {
    url: normalizedUrl,
    token: storedToken,
  };

  return credentialsCache;
};

export const saveCredentials = async ({
  url,
  token,
}: HubCredentials): Promise<void> => {
  const normalizedUrl = normalizeHubUrl(url);

  await Promise.all([
    SecureStore.setItemAsync(hubUrlKey, normalizedUrl),
    SecureStore.setItemAsync(hubTokenKey, token),
  ]);

  credentialsCache = {
    url: normalizedUrl,
    token,
  };
  hubUrlCache = normalizedUrl;
};

export const clearCredentials = async (): Promise<void> => {
  await Promise.all([
    SecureStore.deleteItemAsync(hubUrlKey),
    SecureStore.deleteItemAsync(hubTokenKey),
  ]);

  credentialsCache = null;
  hubUrlCache = null;
};

export const invalidateCredentials = async ({
  url,
}: {
  url?: string | null;
} = {}): Promise<void> => {
  const retainedUrl = url ?? hubUrlCache ?? credentialsCache?.url ?? null;

  if (retainedUrl !== null) {
    const normalizedUrl = normalizeHubUrl(retainedUrl);
    await SecureStore.setItemAsync(hubUrlKey, normalizedUrl);
    hubUrlCache = normalizedUrl;
  }

  await SecureStore.deleteItemAsync(hubTokenKey);
  credentialsCache = null;
};

export const getCredentials = (): HubCredentials | null => credentialsCache;

export const getStoredHubUrl = (): string | null => hubUrlCache;

// Expo SecureStore has no web implementation here. A future credentials.web.ts
// can provide a localStorage-only development escape hatch if web ever needs it.
