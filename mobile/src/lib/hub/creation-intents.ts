import AsyncStorage from '@react-native-async-storage/async-storage';

import type { CreateSessionRequest } from '@/lib/hub/types';

type CreationDraft = {
  hostId: string;
  repoId: string;
  useRawRepo: boolean;
  rawRepo: string;
  agent: string;
  permissionMode: string;
  permissionProfile: string;
  cwd: string;
  branch: string;
  baseRef: string;
  prompt: string;
  model: string;
  reasoning: string;
  workspaceMode: string;
  nativeThreadId: string;
  conversationSearch: string;
};

type StoredCreationIntent = {
  version: 1;
  requestId: string;
  payloadKey: string;
};

const draftKey = (hubUrl: string): string =>
  `tethon:create-draft:v1:${JSON.stringify(hubUrl)}`;

const intentKey = ({
  hubUrl,
  hostId,
}: {
  hubUrl: string;
  hostId: string;
}): string => `tethon:create-intent:v1:${JSON.stringify([hubUrl, hostId])}`;

const newClientRequestId = (): string => {
  if (typeof globalThis.crypto?.randomUUID === 'function') {
    return globalThis.crypto.randomUUID();
  }

  // This UUID identifies an action. It is not an authentication credential.
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (letter) => {
    const random = Math.floor(Math.random() * 16);
    return (letter === 'x' ? random : (random & 3) | 8).toString(16);
  });
};

const loadCreationDraft = async (
  hubUrl: string,
): Promise<CreationDraft | null> => {
  const raw = await AsyncStorage.getItem(draftKey(hubUrl));
  if (raw === null) {
    return null;
  }

  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) {
      return null;
    }

    const candidate = value as Record<string, unknown>;
    const stringFields = [
      'hostId',
      'repoId',
      'rawRepo',
      'agent',
      'permissionMode',
      'cwd',
      'branch',
      'baseRef',
      'prompt',
      'model',
      'reasoning',
      'workspaceMode',
      'nativeThreadId',
      'conversationSearch',
    ];

    if (
      !stringFields.every((field) => typeof candidate[field] === 'string') ||
      typeof candidate.useRawRepo !== 'boolean'
    ) {
      return null;
    }

    return {
      ...candidate,
      permissionProfile:
        typeof candidate.permissionProfile === 'string'
          ? candidate.permissionProfile
          : '',
    } as CreationDraft;
  } catch {
    return null;
  }
};

let pendingDraftWrite: Promise<unknown> = Promise.resolve();

const saveCreationDraft = ({
  hubUrl,
  draft,
}: {
  hubUrl: string;
  draft: CreationDraft;
}): Promise<void> => {
  const operation = pendingDraftWrite.then(() =>
    AsyncStorage.setItem(draftKey(hubUrl), JSON.stringify(draft)),
  );
  pendingDraftWrite = operation.catch(() => undefined);
  return operation;
};

const clearCreationDraft = (hubUrl: string): Promise<void> => {
  const operation = pendingDraftWrite.then(() =>
    AsyncStorage.removeItem(draftKey(hubUrl)),
  );
  pendingDraftWrite = operation.catch(() => undefined);
  return operation;
};

let pendingIntentWrite: Promise<unknown> = Promise.resolve();

const getCreationRequestId = ({
  hubUrl,
  hostId,
  payload,
}: {
  hubUrl: string;
  hostId: string;
  payload: CreateSessionRequest;
}): Promise<string> => {
  const operation = pendingIntentWrite.then(async () => {
    const key = intentKey({ hubUrl, hostId });
    const payloadKey = JSON.stringify(payload);
    const raw = await AsyncStorage.getItem(key);

    if (raw !== null) {
      try {
        const stored = JSON.parse(raw) as Partial<StoredCreationIntent>;
        if (
          stored.version === 1 &&
          stored.payloadKey === payloadKey &&
          typeof stored.requestId === 'string' &&
          /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(
            stored.requestId,
          )
        ) {
          return stored.requestId;
        }
      } catch {
        // A malformed local record cannot identify an earlier action.
      }
    }

    const requestId = newClientRequestId();
    const intent: StoredCreationIntent = { version: 1, requestId, payloadKey };
    await AsyncStorage.setItem(key, JSON.stringify(intent));
    return requestId;
  });

  pendingIntentWrite = operation.catch(() => undefined);
  return operation;
};

const clearCreationIntent = async ({
  hubUrl,
  hostId,
}: {
  hubUrl: string;
  hostId: string;
}): Promise<void> => {
  await pendingIntentWrite;
  await AsyncStorage.removeItem(intentKey({ hubUrl, hostId }));
};

export {
  newClientRequestId,
  loadCreationDraft,
  saveCreationDraft,
  clearCreationDraft,
  getCreationRequestId,
  clearCreationIntent,
  type CreationDraft,
};
