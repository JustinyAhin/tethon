import {
  createContext,
  type PropsWithChildren,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';

import {
  clearCredentials,
  getStoredHubUrl,
  invalidateCredentials,
  loadCredentials,
  normalizeHubUrl,
  saveCredentials,
} from '@/lib/hub/credentials';
import { fetchHosts } from '@/lib/hub/client';
import {
  registerCurrentDevice,
  unregisterCurrentDevice,
} from '@/lib/push/registration';

export type AuthState = {
  status: 'loading' | 'signed-out' | 'signed-in';
  hubUrl: string | null;
};

type SignInArgs = {
  url: string;
  token: string;
};

type AuthContextValue = AuthState & {
  signIn: ({ url, token }: SignInArgs) => Promise<void>;
  signOut: () => Promise<void>;
  invalidate: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export const AuthProvider = ({ children }: PropsWithChildren) => {
  const [state, setState] = useState<AuthState>({
    status: 'loading',
    hubUrl: null,
  });

  useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const credentials = await loadCredentials();

        if (cancelled) {
          return;
        }

        setState({
          status: credentials === null ? 'signed-out' : 'signed-in',
          hubUrl: credentials?.url ?? getStoredHubUrl(),
        });
        if (credentials !== null) {
          void registerCurrentDevice().catch((error: unknown) => {
            console.warn('push registration failed', error);
          });
        }
      } catch {
        if (!cancelled) {
          setState({ status: 'signed-out', hubUrl: null });
        }
      }
    };

    void load();

    return () => {
      cancelled = true;
    };
  }, []);

  const signIn = useCallback(async ({ url, token }: SignInArgs) => {
    const credentials = {
      url: normalizeHubUrl(url),
      token: token.trim(),
    };

    await fetchHosts(credentials);
    await saveCredentials(credentials);
    setState({ status: 'signed-in', hubUrl: credentials.url });
    void registerCurrentDevice().catch((error: unknown) => {
      console.warn('push registration failed', error);
    });
  }, []);

  const signOut = useCallback(async () => {
    await unregisterCurrentDevice().catch((error: unknown) => {
      console.warn('push unregister failed', error);
    });
    await clearCredentials();
    setState({ status: 'signed-out', hubUrl: null });
  }, []);

  const invalidate = useCallback(async () => {
    const hubUrl = state.hubUrl ?? getStoredHubUrl();
    await invalidateCredentials({ url: hubUrl });
    setState({ status: 'signed-out', hubUrl });
  }, [state.hubUrl]);

  const value = useMemo<AuthContextValue>(
    () => ({
      ...state,
      signIn,
      signOut,
      invalidate,
    }),
    [invalidate, signIn, signOut, state],
  );

  return <AuthContext value={value}>{children}</AuthContext>;
};

export const useAuth = (): AuthContextValue => {
  const value = useContext(AuthContext);

  if (value === null) {
    throw new Error('useAuth must be used within AuthProvider');
  }

  return value;
};
