import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useFocusEffect } from 'expo-router';

import { HubAuthError, HubRequestError } from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import { loadSessionEvents, saveSessionEvents } from '@/lib/hub/event-storage';
import { openSessionStream } from '@/lib/hub/stream';
import type { SessionEvent } from '@/lib/hub/types';
import { dismissApprovalNotifications } from '@/lib/push/resolved';

type UseSessionStreamArgs = {
  hostId: string;
  localId: string;
  persistent?: boolean;
};

type StreamStatus = 'connecting' | 'live' | 'finished' | 'error';

type UseSessionStreamResult = {
  events: SessionEvent[];
  status: StreamStatus;
  streamError: string | null;
};

const reconnectDelayMs = 1000;

const dismissResolvedEventNotification = ({
  event,
  hostId,
  localId,
}: {
  event: SessionEvent;
  hostId: string;
  localId: string;
}): void => {
  if (
    event.type !== 'approval_resolved' &&
    event.type !== 'codex_request_resolved'
  ) {
    return;
  }

  if (typeof event.payload !== 'object' || event.payload === null) {
    return;
  }

  const payload = event.payload as Record<string, unknown>;
  let requestId: string;

  if (event.type === 'codex_request_resolved') {
    if (
      typeof payload.request_key !== 'string' ||
      typeof payload.connection_id !== 'string'
    ) {
      return;
    }

    requestId = `${payload.connection_id}:${payload.request_key}`;
  } else {
    if (typeof payload.request_id !== 'string') {
      return;
    }

    requestId = payload.request_id;
  }

  void dismissApprovalNotifications({ hostId, sessionId: localId, requestId });
};

export const useSessionStream = ({
  hostId,
  localId,
  persistent = false,
}: UseSessionStreamArgs): UseSessionStreamResult => {
  const [events, setEvents] = useState<SessionEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [done, setDone] = useState(false);
  const [streamError, setStreamError] = useState<string | null>(null);
  const { invalidate, status: authStatus } = useAuth();
  const seenRef = useRef(new Set<number>());
  const maxSeqRef = useRef(0);
  const doneRef = useRef(done);
  const hydratedRef = useRef(false);
  const controllerRef = useRef<AbortController | null>(null);

  const status = useMemo<StreamStatus>(() => {
    if (streamError !== null) {
      return 'error';
    }

    if (done) {
      return 'finished';
    }

    if (connected) {
      return 'live';
    }

    return 'connecting';
  }, [connected, done, streamError]);

  useFocusEffect(
    useCallback(() => {
      if (authStatus !== 'signed-in') {
        return;
      }

      let stopped = false;

      const sleep = async (ms: number) => {
        await new Promise((resolve) => {
          const timeout = setTimeout(resolve, ms);

          if (stopped) {
            clearTimeout(timeout);
            resolve(undefined);
          }
        });
      };

      const parseEvent = (frame: string): SessionEvent | null => {
        for (const line of frame.split('\n')) {
          if (!line.startsWith('data:')) {
            continue;
          }

          try {
            return JSON.parse(line.slice(5).trimStart()) as SessionEvent;
          } catch {
            return null;
          }
        }

        return null;
      };

      const hydrate = async (): Promise<boolean> => {
        if (hydratedRef.current) {
          return true;
        }

        const stored = await loadSessionEvents({ hostId, localId });

        if (stopped) {
          return false;
        }

        if (stored.length > 0) {
          const seen = new Set(stored.map((event) => event.seq));
          const maxSeq = stored.reduce(
            (max, event) => Math.max(max, event.seq),
            0,
          );
          const storedDone = stored.some(
            (event) =>
              !persistent && (event.type === 'done' || event.type === 'error'),
          );

          seenRef.current = seen;
          maxSeqRef.current = maxSeq;
          doneRef.current = storedDone;
          setDone(storedDone);
          setEvents(stored);

          for (const event of stored) {
            dismissResolvedEventNotification({ event, hostId, localId });
          }
        }

        hydratedRef.current = true;
        return true;
      };

      const consume = async () => {
        if (doneRef.current) {
          return;
        }

        controllerRef.current?.abort();
        const controller = new AbortController();
        controllerRef.current = controller;
        setStreamError(null);

        try {
          const response = await openSessionStream({
            hostId,
            localId,
            from: maxSeqRef.current,
            signal: controller.signal,
          });

          const reader = response.body?.getReader();

          if (reader === undefined) {
            throw new HubRequestError({
              message: 'Hub stream response did not include a body',
            });
          }

          setConnected(true);
          const decoder = new TextDecoder();
          let buffer = '';

          for (;;) {
            const { done: chunkDone, value } = await reader.read();

            if (chunkDone) {
              break;
            }

            buffer += decoder.decode(value, { stream: true });
            const batch: SessionEvent[] = [];
            let sep: number;

            while ((sep = buffer.indexOf('\n\n')) !== -1) {
              const event = parseEvent(buffer.slice(0, sep));
              buffer = buffer.slice(sep + 2);

              if (event === null || seenRef.current.has(event.seq)) {
                continue;
              }

              seenRef.current.add(event.seq);
              maxSeqRef.current = Math.max(maxSeqRef.current, event.seq);
              batch.push(event);
              dismissResolvedEventNotification({ event, hostId, localId });

              if (
                !persistent &&
                (event.type === 'done' || event.type === 'error')
              ) {
                doneRef.current = true;
                setDone(true);
              }
            }

            if (batch.length > 0) {
              setEvents((current) => current.concat(batch));
            }
          }
        } catch (caught) {
          if (controller.signal.aborted) {
            return;
          }

          if (caught instanceof HubAuthError) {
            stopped = true;
            await invalidate();
            return;
          }

          setStreamError(
            caught instanceof Error ? caught.message : 'Stream disconnected',
          );
        } finally {
          if (controllerRef.current === controller) {
            setConnected(false);
            controllerRef.current = null;
          }
        }
      };

      void (async () => {
        const hydrated = await hydrate();

        if (!hydrated) {
          return;
        }

        while (!stopped && !doneRef.current) {
          await consume();

          if (stopped || doneRef.current) {
            break;
          }

          await sleep(reconnectDelayMs);
        }
      })();

      return () => {
        stopped = true;
        controllerRef.current?.abort();
      };
    }, [authStatus, hostId, invalidate, localId, persistent]),
  );

  useEffect(() => {
    if (!hydratedRef.current || events.length === 0) {
      return;
    }

    const timeout = setTimeout(() => {
      void saveSessionEvents({ events, hostId, localId });
    }, 400);

    return () => {
      clearTimeout(timeout);
    };
  }, [events, hostId, localId]);

  return { events, status, streamError };
};
