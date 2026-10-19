import { useCallback, useMemo, useRef, useState } from 'react';
import { useFocusEffect } from 'expo-router';

import {
  fetchNativeHistory,
  fetchNativeSession,
  HubAuthError,
} from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import type {
  CodexSessionState,
  CodexRequestPayload,
  CodexRequestResolvedPayload,
  SessionEvent,
} from '@/lib/hub/types';
import type { NativeTurn } from '@/lib/hub/codex-timeline';

type UseCodexSessionArgs = {
  hostId: string;
  localId: string;
  initialState?: CodexSessionState;
  events: SessionEvent[];
  enabled: boolean;
  streamStatus: string;
};

const requestIdentity = (request: {
  request_key: string;
  connection_id: string;
}) => JSON.stringify([request.connection_id, request.request_key]);

const useCodexSession = ({
  hostId,
  localId,
  initialState,
  events,
  enabled,
  streamStatus,
}: UseCodexSessionArgs) => {
  const { invalidate } = useAuth();
  const [snapshot, setSnapshot] = useState<Awaited<
    ReturnType<typeof fetchNativeSession>
  > | null>(null);
  const [turns, setTurns] = useState<NativeTurn[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [error, setError] = useState<string | null>(null);
  const [snapshotValid, setSnapshotValid] = useState(false);
  const fetching = useRef(false);
  const historyFetching = useRef(false);
  const historyLoaded = useRef(false);

  const refreshSignal = useMemo(
    () =>
      events
        .filter(
          (event) =>
            event.type === 'codex_state' ||
            (event.type === 'codex_event' &&
              ['turn/completed', 'thread/queue/changed'].includes(
                (event.payload as { method: string }).method,
              )),
        )
        .at(-1)?.seq ?? 0,
    [events],
  );

  const refresh = useCallback(async () => {
    if (!enabled || fetching.current) {
      return;
    }

    fetching.current = true;

    try {
      const next = await fetchNativeSession({ hostId, localId });

      setSnapshot(next);
      setSnapshotValid(true);
      setError(null);
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setError(
        'Could not refresh this conversation. Check the host connection.',
      );
      setSnapshotValid(false);
    } finally {
      fetching.current = false;
    }
  }, [enabled, hostId, invalidate, localId]);

  const loadHistory = useCallback(
    async (pageCursor?: string) => {
      if (!enabled || historyFetching.current) {
        return;
      }

      historyFetching.current = true;

      try {
        const page = await fetchNativeHistory({
          hostId,
          localId,
          cursor: pageCursor,
        });
        const incoming = [...(page.turns ?? [])].reverse();

        setTurns((current) => {
          const merged = new Map<string, NativeTurn>();
          const ordered = pageCursor
            ? [...incoming, ...current]
            : [...current, ...incoming];

          for (const turn of ordered) {
            merged.set(turn.id, turn);
          }

          return [...merged.values()];
        });

        if (pageCursor !== undefined || !historyLoaded.current) {
          setCursor(page.next_cursor);
        }

        historyLoaded.current = true;
      } catch (caught) {
        if (caught instanceof HubAuthError) {
          await invalidate();
          return;
        }

        setError(
          'Could not load conversation history. Retry when the host is online.',
        );
      } finally {
        historyFetching.current = false;
      }
    },
    [enabled, hostId, invalidate, localId],
  );

  useFocusEffect(
    useCallback(() => {
      if (!enabled || streamStatus === 'finished') {
        return;
      }

      void refresh();
      void loadHistory();
      const timer = setInterval(() => {
        void refresh();
      }, 10_000);

      return () => clearInterval(timer);
    }, [enabled, refresh, loadHistory, streamStatus]),
  );

  useFocusEffect(
    useCallback(() => {
      if (!enabled || refreshSignal === 0) {
        return;
      }

      void refresh();
      void loadHistory();
    }, [enabled, refreshSignal, refresh, loadHistory]),
  );

  const { state, requests } = useMemo(() => {
    let currentState = snapshot?.state ?? initialState;
    const pending = new Map(
      (snapshot?.pending_requests ?? []).map((request) => [
        requestIdentity(request),
        request,
      ]),
    );

    for (const event of events) {
      // The snapshot cursor prevents old delivery events from restoring resolved requests.
      if (event.seq <= (snapshot?.seq ?? 0)) {
        continue;
      }

      if (event.type === 'codex_state') {
        currentState = event.payload as CodexSessionState;
      }

      if (event.type === 'codex_request') {
        const request = event.payload as CodexRequestPayload;
        pending.set(requestIdentity(request), request);
      }

      if (event.type === 'codex_request_resolved') {
        const request = event.payload as CodexRequestResolvedPayload;
        pending.delete(requestIdentity(request));
      }
    }

    return { state: currentState, requests: [...pending.values()] };
  }, [events, initialState, snapshot]);

  const reconciled =
    snapshotValid &&
    streamStatus === 'live' &&
    snapshot?.state.connection_id === state?.connection_id;

  return {
    state,
    requests,
    turns,
    error,
    refresh,
    loadOlder: () => loadHistory(cursor),
    hasOlder: cursor !== undefined,
    queue: snapshot?.queue ?? [],
    queueAvailable: snapshot?.queue_available === true,
    workspaceOwned: snapshot?.workspace_owned === true,
    reconciled,
    ready:
      reconciled &&
      state?.connection_state === 'connected' &&
      state.conversation_state === 'ready',
  };
};

export { useCodexSession, requestIdentity };
