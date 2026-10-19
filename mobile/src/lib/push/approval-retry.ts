import type { ApprovalAction } from '@/lib/hub/types';

type ApprovalIdentity = {
  hostId: string;
  sessionId: string;
  requestId: string;
};
type ApprovalIntent = ApprovalIdentity &
  (
    | {
        kind?: 'legacy';
        action: ApprovalAction;
        text?: string;
      }
    | { kind: 'native'; connectionId: string; result: unknown }
  );

type StoredApprovalIntent = ApprovalIntent & {
  hubUrl: string;
  responseKey: string;
  createdAt: number;
  attempts: number;
  nextAttemptAt: number;
};

type RetryState = {
  version: 1;
  pending: StoredApprovalIntent[];
  processed: string[];
  settled: string[];
};

type RetryStorage = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
};

type ResolutionOutcome = 'resolved' | 'stale' | 'retry' | 'pause';

const storageKey = 'tethon:approval-intents:v1';
const maxStoredIntents = 80;
const maxProcessedResponses = 80;
const maxIntentAgeMs = 24 * 60 * 60 * 1_000;
const maxAttempts = 20;
const maxRetryDelayMs = 5 * 60 * 1_000;

const requestKey = (intent: ApprovalIntent): string =>
  JSON.stringify([
    intent.hostId,
    intent.sessionId,
    intent.requestId,
    intent.kind === 'native' ? intent.connectionId : '',
  ]);

const scopedRequestKey = (
  intent: ApprovalIntent & { hubUrl: string },
): string => JSON.stringify([intent.hubUrl, requestKey(intent)]);

const retryDelay = (attempts: number): number =>
  Math.min(5_000 * 2 ** (attempts - 1), maxRetryDelayMs);

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null;

const isStoredIntent = (value: unknown): value is StoredApprovalIntent => {
  if (!isRecord(value)) {
    return false;
  }

  return (
    ['hostId', 'sessionId', 'requestId', 'hubUrl', 'responseKey'].every(
      (key) => typeof value[key] === 'string' && value[key].length > 0,
    ) &&
    (value.kind === 'native'
      ? typeof value.connectionId === 'string' &&
        value.connectionId.length > 0 &&
        Object.hasOwn(value, 'result')
      : value.action === 'approve' ||
        value.action === 'deny' ||
        (value.action === 'reply' &&
          typeof value.text === 'string' &&
          value.text.length > 0)) &&
    (value.text === undefined || typeof value.text === 'string') &&
    typeof value.createdAt === 'number' &&
    Number.isFinite(value.createdAt) &&
    typeof value.nextAttemptAt === 'number' &&
    Number.isFinite(value.nextAttemptAt) &&
    typeof value.attempts === 'number' &&
    Number.isInteger(value.attempts) &&
    value.attempts >= 0
  );
};

const parseState = (raw: string | null): RetryState => {
  try {
    const value: unknown = raw === null ? null : JSON.parse(raw);
    if (
      isRecord(value) &&
      value.version === 1 &&
      Array.isArray(value.pending) &&
      Array.isArray(value.processed)
    ) {
      return {
        version: 1,
        pending: value.pending.filter(isStoredIntent).slice(-maxStoredIntents),
        processed: value.processed
          .filter((key): key is string => typeof key === 'string')
          .slice(-maxProcessedResponses),
        settled: Array.isArray(value.settled)
          ? value.settled
              .filter((key): key is string => typeof key === 'string')
              .slice(-maxProcessedResponses)
          : [],
      };
    }
  } catch {
    // Ignore malformed local data. Never send a partial action.
  }

  return { version: 1, pending: [], processed: [], settled: [] };
};

const createApprovalRetryQueue = ({
  storage,
  now = Date.now,
}: {
  storage: RetryStorage;
  now?: () => number;
}) => {
  let state: RetryState | null = null;
  let serial: Promise<unknown> = Promise.resolve();
  let flushing: Promise<void> | null = null;

  const run = <T>(
    operation: (current: RetryState) => Promise<T>,
  ): Promise<T> => {
    const next = serial.then(async () => {
      state ??= parseState(await storage.getItem(storageKey));
      return operation(state);
    });
    serial = next.catch(() => undefined);
    return next;
  };

  const save = async (next: RetryState): Promise<void> => {
    await storage.setItem(storageKey, JSON.stringify(next));
    state = next;
  };

  const isCurrent = (intent: StoredApprovalIntent): boolean =>
    intent.createdAt <= now() &&
    now() - intent.createdAt < maxIntentAgeMs &&
    intent.attempts < maxAttempts;

  const enqueue = ({
    hubUrl,
    responseKey,
    intent,
  }: {
    hubUrl: string;
    responseKey: string;
    intent: ApprovalIntent;
  }): Promise<void> =>
    run(async (current) => {
      if (current.processed.includes(responseKey)) {
        return;
      }

      const pending = current.pending.filter(isCurrent);
      if (
        !current.settled.includes(scopedRequestKey({ ...intent, hubUrl })) &&
        !pending.some(
          (item) =>
            item.hubUrl === hubUrl && requestKey(item) === requestKey(intent),
        )
      ) {
        pending.push({
          ...intent,
          hubUrl,
          responseKey,
          createdAt: now(),
          attempts: 0,
          nextAttemptAt: now(),
        });
      }

      await save({
        version: 1,
        pending: pending.slice(-maxStoredIntents),
        processed: [...current.processed, responseKey].slice(
          -maxProcessedResponses,
        ),
        settled: current.settled,
      });
    });

  const clear = (): Promise<void> =>
    run(async (current) => {
      await save({ ...current, pending: [] });
    });

  const flush = ({
    hubUrl,
    canRun,
    resolve,
  }: {
    hubUrl: string;
    canRun: () => boolean;
    resolve: (intent: ApprovalIntent) => Promise<ResolutionOutcome>;
  }): Promise<void> => {
    if (flushing !== null) {
      return flushing;
    }

    flushing = (async () => {
      const pending = await run(async (current) => {
        const next = {
          ...current,
          pending: current.pending.filter(
            (intent) => intent.hubUrl === hubUrl && isCurrent(intent),
          ),
        };
        await save(next);
        return next.pending;
      });

      for (const intent of pending) {
        const attempted = await run(async (current) => {
          const queued = current.pending.find(
            (item) => scopedRequestKey(item) === scopedRequestKey(intent),
          );
          if (
            !canRun() ||
            queued === undefined ||
            queued.nextAttemptAt > now()
          ) {
            return null;
          }

          // Save the attempt before sending. A restart must retain the same request.
          const next = {
            ...queued,
            attempts: queued.attempts + 1,
            nextAttemptAt: now() + retryDelay(queued.attempts + 1),
          };
          await save({
            ...current,
            pending: current.pending.map((item) =>
              item === queued ? next : item,
            ),
          });
          return next;
        });
        if (attempted === null || !canRun()) {
          continue;
        }

        const outcome = await resolve(attempted);
        if (outcome === 'pause') {
          return;
        }
        if (outcome !== 'retry' || !isCurrent(attempted)) {
          await run(async (current) => {
            await save({
              ...current,
              pending: current.pending.filter(
                (item) =>
                  scopedRequestKey(item) !== scopedRequestKey(attempted),
              ),
              settled:
                outcome === 'retry'
                  ? current.settled
                  : [...current.settled, scopedRequestKey(attempted)].slice(
                      -maxProcessedResponses,
                    ),
            });
          });
        }
      }
    })().finally(() => {
      flushing = null;
    });
    return flushing;
  };

  const nextAttemptAt = (): Promise<number | null> =>
    run(async (current) => {
      const pending = current.pending.filter(isCurrent);
      return pending.length === 0
        ? null
        : Math.min(...pending.map((intent) => intent.nextAttemptAt));
    });

  return { enqueue, clear, flush, nextAttemptAt };
};

export {
  createApprovalRetryQueue,
  type ApprovalIntent,
  type ResolutionOutcome,
};
