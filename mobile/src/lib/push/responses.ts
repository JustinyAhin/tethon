import AsyncStorage from '@react-native-async-storage/async-storage';
import * as Notifications from 'expo-notifications';
import { router } from 'expo-router';
import { AppState } from 'react-native';

import {
  HubAuthError,
  HubRequestError,
  resolveApproval,
  respondToNativeRequest,
} from '@/lib/hub/client';
import {
  getCredentials,
  invalidateCredentials,
  loadCredentials,
} from '@/lib/hub/credentials';
import type { ApprovalAction } from '@/lib/hub/types';
import {
  createApprovalRetryQueue,
  type ApprovalIntent,
  type ResolutionOutcome,
} from '@/lib/push/approval-retry';
import { approvalNotificationActionIDs } from '@/lib/push/registration';
import { dismissApprovalNotifications } from '@/lib/push/resolved';

type NotificationAuthStatus = 'loading' | 'signed-out' | 'signed-in';

type ApprovalNotificationData = {
  hostId: string;
  requestId: string;
  sessionId: string;
  requestKey?: string;
  connectionId?: string;
};

type ParsedNotificationResponse =
  | ({
      kind: 'approval_action';
      action: ApprovalAction;
      text?: string;
    } & ApprovalNotificationData)
  | ({
      kind: 'open_session';
    } & ApprovalNotificationData);

const maxProcessedResponses = 80;

let authStatus: NotificationAuthStatus = 'loading';
let onAuthInvalidated: (() => Promise<void> | void) | null = null;
let processing = false;
let retrying = false;
let appActive =
  AppState.currentState === null || AppState.currentState === 'active';
let retryTimer: ReturnType<typeof setTimeout> | null = null;

const approvalRetries = createApprovalRetryQueue({ storage: AsyncStorage });

const pendingResponses: ParsedNotificationResponse[] = [];
const processedResponseKeys: string[] = [];
const processedResponseKeySet = new Set<string>();
const processingResponses = new Map<string, Promise<void>>();

const setNotificationResponseAuthStatus = (
  status: NotificationAuthStatus,
): void => {
  authStatus = status;

  if (status === 'signed-out') {
    pendingResponses.length = 0;
    clearRetryTimer();
    void approvalRetries.clear().catch(reportRetryError);
    return;
  }

  if (status === 'signed-in') {
    void processPendingResponses();
    void retryPendingApprovals();
  }
};

const setNotificationResponseAuthInvalidatedHandler = (
  handler: (() => Promise<void> | void) | null,
): void => {
  onAuthInvalidated = handler;
};

const handleLastNotificationResponse = async (): Promise<void> => {
  const response = Notifications.getLastNotificationResponse();
  if (response === null) {
    return;
  }

  await handleNotificationResponse(response);
  const current = Notifications.getLastNotificationResponse();
  if (
    current?.notification.request.identifier ===
      response.notification.request.identifier &&
    current.actionIdentifier === response.actionIdentifier &&
    current.userText === response.userText
  ) {
    Notifications.clearLastNotificationResponse();
  }
};

const handleNotificationResponse = async (
  response: Notifications.NotificationResponse,
): Promise<void> => {
  const parsed = parseNotificationResponse(response);
  if (parsed === null) {
    return;
  }

  const key = responseKey(parsed);
  const inFlight = processingResponses.get(key);
  if (inFlight !== undefined) {
    await inFlight;
    return;
  }
  if (hasProcessedResponse(key)) {
    return;
  }
  rememberProcessedResponse(key);

  if (authStatus === 'loading' && parsed.kind === 'open_session') {
    pendingResponses.push(parsed);
    return;
  }

  if (authStatus === 'signed-out') {
    return;
  }

  const operation = processParsedResponse(parsed);
  processingResponses.set(key, operation);
  try {
    await operation;
  } catch (error) {
    // Keep the native last response available until the action is stored.
    processedResponseKeySet.delete(key);
    const index = processedResponseKeys.indexOf(key);
    if (index !== -1) {
      processedResponseKeys.splice(index, 1);
    }
    throw error;
  } finally {
    processingResponses.delete(key);
  }
};

const processPendingResponses = async (): Promise<void> => {
  if (processing) {
    return;
  }

  processing = true;
  try {
    while (authStatus === 'signed-in' && pendingResponses.length > 0) {
      const response = pendingResponses.shift();
      if (response !== undefined) {
        await processParsedResponse(response);
      }
    }
  } finally {
    processing = false;
  }
};

const processParsedResponse = async (
  response: ParsedNotificationResponse,
): Promise<void> => {
  if (getCredentials() === null) {
    const credentials = await loadCredentials();
    if (credentials === null) {
      pendingResponses.length = 0;
      setNotificationResponseAuthStatus('signed-out');
      await onAuthInvalidated?.();
      return;
    }
  }

  if (response.kind === 'open_session') {
    router.push({
      pathname: '/sessions/[host]/[local]',
      params: {
        host: response.hostId,
        local: response.sessionId,
        requestKey: response.requestKey,
        connectionId: response.connectionId,
      },
    });
    return;
  }

  const credentials = getCredentials();
  if (credentials === null || authStatus === 'signed-out') {
    return;
  }

  await approvalRetries.enqueue({
    hubUrl: credentials.url,
    responseKey: responseKey(response),
    intent: {
      hostId: response.hostId,
      sessionId: response.sessionId,
      requestId: response.requestId,
      action: response.action,
      text: response.text,
    },
  });
  void retryPendingApprovals();
};

const clearRetryTimer = (): void => {
  if (retryTimer !== null) {
    clearTimeout(retryTimer);
    retryTimer = null;
  }
};

const reportRetryError = (error: unknown): void => {
  console.warn('approval notification retry storage failed', error);
};

const sendApprovalIntent = async (
  intent: ApprovalIntent,
): Promise<ResolutionOutcome> => {
  try {
    if (intent.kind === 'native') {
      await respondToNativeRequest({
        hostId: intent.hostId,
        localId: intent.sessionId,
        requestKey: intent.requestId,
        connectionId: intent.connectionId,
        result: intent.result,
      });
    } else {
      await resolveApproval({
        action: intent.action,
        hostId: intent.hostId,
        localId: intent.sessionId,
        requestId: intent.requestId,
        text: intent.text,
      });
    }

    await dismissApprovalNotifications({
      ...intent,
      requestId:
        intent.kind === 'native'
          ? `${intent.connectionId}:${intent.requestId}`
          : intent.requestId,
    });

    return 'resolved';
  } catch (error) {
    if (error instanceof HubAuthError) {
      await invalidateNotificationAuth();
      return 'pause';
    }

    if (
      error instanceof HubRequestError &&
      (error.status === 404 || error.status === 409)
    ) {
      await dismissApprovalNotifications({
        ...intent,
        requestId:
          intent.kind === 'native'
            ? `${intent.connectionId}:${intent.requestId}`
            : intent.requestId,
      });
      return 'stale';
    }

    if (
      error instanceof HubRequestError &&
      error.status !== undefined &&
      error.status < 500 &&
      error.status !== 408 &&
      error.status !== 429
    ) {
      console.warn('approval notification response rejected', {
        requestId: intent.requestId,
        status: error.status,
      });
      return 'stale';
    }

    return 'retry';
  }
};

const retryPendingApprovals = async (): Promise<void> => {
  if (retrying || !appActive || authStatus !== 'signed-in') {
    return;
  }

  const credentials = getCredentials();

  if (credentials === null) {
    return;
  }

  retrying = true;
  clearRetryTimer();
  let nextAttempt: number | null = null;

  try {
    await approvalRetries.flush({
      hubUrl: credentials.url,
      canRun: () =>
        appActive &&
        authStatus === 'signed-in' &&
        getCredentials() === credentials,
      resolve: sendApprovalIntent,
    });
    nextAttempt = await approvalRetries.nextAttemptAt();
  } catch (error) {
    reportRetryError(error);
    nextAttempt = Date.now() + 5_000;
  } finally {
    retrying = false;
  }

  if (nextAttempt !== null && appActive && authStatus === 'signed-in') {
    retryTimer = setTimeout(
      () => {
        retryTimer = null;
        void retryPendingApprovals();
      },
      Math.max(1_000, nextAttempt - Date.now()),
    );
  }
};

const invalidateNotificationAuth = async (): Promise<void> => {
  await invalidateCredentials();
  setNotificationResponseAuthStatus('signed-out');
  await onAuthInvalidated?.();
};

const parseNotificationResponse = (
  response: Notifications.NotificationResponse,
): ParsedNotificationResponse | null => {
  const data = response.notification.request.content.data;
  if (data === undefined) {
    return null;
  }

  if (data.kind !== 'approval_requested') {
    return null;
  }

  const hostId = stringValue(data.host_id);
  const sessionId = stringValue(data.session_id);
  const requestId = stringValue(data.request_id);

  if (hostId === null || sessionId === null || requestId === null) {
    return null;
  }

  const requestKey = stringValue(data.request_key) ?? undefined;
  const connectionId = stringValue(data.connection_id) ?? undefined;
  const base = { hostId, requestId, sessionId, requestKey, connectionId };

  if (data.agent === 'codex-cli' || requestKey !== undefined) {
    return { ...base, kind: 'open_session' };
  }

  switch (response.actionIdentifier) {
    case Notifications.DEFAULT_ACTION_IDENTIFIER:
      return { ...base, kind: 'open_session' };
    case approvalNotificationActionIDs.approve:
      return { ...base, kind: 'approval_action', action: 'approve' };
    case approvalNotificationActionIDs.deny:
      return { ...base, kind: 'approval_action', action: 'deny' };
    case approvalNotificationActionIDs.reply: {
      const text = response.userText?.trim();
      if (text === undefined || text.length === 0) {
        return null;
      }
      return { ...base, kind: 'approval_action', action: 'reply', text };
    }
    default:
      return null;
  }
};

const stringValue = (value: unknown): string | null =>
  typeof value === 'string' && value.length > 0 ? value : null;

const queueNativeApprovalResponse = async ({
  hostId,
  sessionId,
  requestKey,
  connectionId,
  result,
}: {
  hostId: string;
  sessionId: string;
  requestKey: string;
  connectionId: string;
  result: unknown;
}): Promise<void> => {
  const credentials = getCredentials();
  if (credentials === null || authStatus !== 'signed-in')
    throw new HubAuthError();
  await approvalRetries.enqueue({
    hubUrl: credentials.url,
    responseKey: JSON.stringify([
      'native',
      hostId,
      sessionId,
      connectionId,
      requestKey,
      result,
    ]),
    intent: {
      kind: 'native',
      hostId,
      sessionId,
      requestId: requestKey,
      connectionId,
      result,
    },
  });
  void retryPendingApprovals();
};

const responseKey = (response: ParsedNotificationResponse): string => {
  if (response.kind === 'open_session') {
    return JSON.stringify([
      response.kind,
      response.hostId,
      response.sessionId,
      response.requestId,
    ]);
  }

  return JSON.stringify([
    response.kind,
    response.hostId,
    response.sessionId,
    response.requestId,
    response.action,
    response.text ?? '',
  ]);
};

const hasProcessedResponse = (key: string): boolean =>
  processedResponseKeySet.has(key);

const rememberProcessedResponse = (key: string): void => {
  processedResponseKeys.push(key);
  processedResponseKeySet.add(key);

  if (processedResponseKeys.length <= maxProcessedResponses) {
    return;
  }

  const oldKey = processedResponseKeys.shift();
  if (oldKey !== undefined) {
    processedResponseKeySet.delete(oldKey);
  }
};

Notifications.addNotificationResponseReceivedListener((response) => {
  void handleNotificationResponse(response).catch(reportRetryError);
});

AppState.addEventListener('change', (state) => {
  appActive = state === 'active';
  if (appActive) {
    void handleLastNotificationResponse().catch(reportRetryError);
    void retryPendingApprovals();
  } else {
    clearRetryTimer();
  }
});

void handleLastNotificationResponse().catch(reportRetryError);

export {
  setNotificationResponseAuthStatus,
  setNotificationResponseAuthInvalidatedHandler,
  queueNativeApprovalResponse,
};
