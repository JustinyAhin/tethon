import { useCallback, useRef, useState } from 'react';
import { useFocusEffect } from 'expo-router';

import { useAuth } from '@/lib/hub/auth-context';
import { fetchSessions, HubAuthError, HubRequestError } from '@/lib/hub/client';
import type { SessionSummary } from '@/lib/hub/types';

type UseSessionsResult = {
  sessions: SessionSummary[];
  loading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
};

const pollMs = 5_000;

export const useSessions = (): UseSessionsResult => {
  const { invalidate, status } = useAuth();
  const [sessions, setSessions] = useState<SessionSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const loadedOnce = useRef(false);

  const load = useCallback(
    async ({ cancelled }: { cancelled: () => boolean }) => {
      if (!loadedOnce.current && !cancelled()) {
        setLoading(true);
      }

      try {
        const response = await fetchSessions();

        if (cancelled()) {
          return;
        }

        setSessions(response.sessions);
        setError(null);
        loadedOnce.current = true;
      } catch (caught) {
        if (cancelled()) {
          return;
        }

        if (caught instanceof HubAuthError) {
          await invalidate();
          return;
        }

        if (!cancelled()) {
          setError(toSessionsErrorMessage(caught));
        }
      } finally {
        if (!cancelled()) {
          setLoading(false);
        }
      }
    },
    [invalidate],
  );

  useFocusEffect(
    useCallback(() => {
      if (status !== 'signed-in') {
        return undefined;
      }

      let cancelled = false;
      const isCancelled = () => cancelled;

      void load({ cancelled: isCancelled });
      const interval = setInterval(() => {
        void load({ cancelled: isCancelled });
      }, pollMs);

      return () => {
        cancelled = true;
        clearInterval(interval);
      };
    }, [load, status]),
  );

  const refresh = useCallback(
    async () => {
      let cancelled = false;
      await load({ cancelled: () => cancelled });
      cancelled = true;
    },
    [load],
  );

  return { sessions, loading, error, refresh };
};

const toSessionsErrorMessage = (error: unknown): string => {
  if (error instanceof HubRequestError) {
    if (error.status === undefined) {
      return 'Could not reach hub';
    }

    return `Hub returned HTTP ${error.status}`;
  }

  return 'Could not load sessions';
};
