import * as Notifications from 'expo-notifications';
import { router } from 'expo-router';

import {
  HubAuthError,
  HubRequestError,
  resolveApproval,
} from '@/lib/hub/client';
import {
  getCredentials,
  invalidateCredentials,
  loadCredentials,
} from '@/lib/hub/credentials';
import type { ApprovalAction } from '@/lib/hub/types';
import { approvalNotificationActionIDs } from '@/lib/push/registration';

type NotificationAuthStatus = 'loading' | 'signed-out' | 'signed-in';

type ApprovalNotificationData = {
  hostId: string;
  requestId: string;
  sessionId: string;
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

const pendingResponses: ParsedNotificationResponse[] = [];
const processedResponseKeys: string[] = [];
const processedResponseKeySet = new Set<string>();

export const setNotificationResponseAuthStatus = (
  status: NotificationAuthStatus,
): void => {
  authStatus = status;

  if (status === 'signed-out') {
    pendingResponses.length = 0;
    return;
  }

  if (status === 'signed-in') {
    void processPendingResponses();
  }
};

export const setNotificationResponseAuthInvalidatedHandler = (
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
  Notifications.clearLastNotificationResponse();
};

const handleNotificationResponse = async (
  response: Notifications.NotificationResponse,
): Promise<void> => {
  const parsed = parseNotificationResponse(response);
  if (parsed === null) {
    return;
  }

  const key = responseKey(parsed);
  if (hasProcessedResponse(key)) {
    return;
  }
  rememberProcessedResponse(key);

  if (authStatus === 'loading') {
    pendingResponses.push(parsed);
    return;
  }

  if (authStatus === 'signed-out') {
    return;
  }

  await processParsedResponse(parsed);
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
      authStatus = 'signed-out';
      await onAuthInvalidated?.();
      return;
    }
  }

  if (response.kind === 'open_session') {
    router.push({
      pathname: '/sessions/[host]/[local]',
      params: { host: response.hostId, local: response.sessionId },
    });
    return;
  }

  try {
    await resolveApproval({
      action: response.action,
      hostId: response.hostId,
      localId: response.sessionId,
      requestId: response.requestId,
      text: response.text,
    });
  } catch (error) {
    if (error instanceof HubAuthError) {
      await invalidateNotificationAuth();
      return;
    }

    if (
      error instanceof HubRequestError &&
      (error.status === 404 || error.status === 409)
    ) {
      console.warn('stale approval notification response ignored', {
        action: response.action,
        requestId: response.requestId,
        status: error.status,
      });
      return;
    }

    console.warn('approval notification response failed', error);
  }
};

const invalidateNotificationAuth = async (): Promise<void> => {
  await invalidateCredentials();
  pendingResponses.length = 0;
  authStatus = 'signed-out';
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

  const base = { hostId, requestId, sessionId };
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

const responseKey = (response: ParsedNotificationResponse): string => {
  if (response.kind === 'open_session') {
    return [
      response.kind,
      response.hostId,
      response.sessionId,
      response.requestId,
    ].join(':');
  }

  return [
    response.kind,
    response.hostId,
    response.sessionId,
    response.requestId,
    response.action,
    response.text ?? '',
  ].join(':');
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
  void handleNotificationResponse(response);
});

void handleLastNotificationResponse();
