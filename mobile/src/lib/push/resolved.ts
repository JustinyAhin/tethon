import AsyncStorage from '@react-native-async-storage/async-storage';
import * as Notifications from 'expo-notifications';
import { Platform } from 'react-native';

export type ApprovalNotificationIdentity = {
  hostId: string;
  sessionId: string;
  requestId: string;
};

const resolvedStorageKey = 'tethon.push-resolved-approvals';
const maxTrackedApprovals = 256;
const deliveredIdentifiers = new Map<string, Set<string>>();
let resolvedKeys: Set<string> | null = null;
let pendingOperation = Promise.resolve();

// Serialize local storage and notification changes across stream and push events.
const enqueue = (operation: () => Promise<void>): Promise<void> => {
  const result = pendingOperation.then(operation);
  pendingOperation = result.catch(() => undefined);
  return result;
};

const identityKey = (identity: ApprovalNotificationIdentity): string =>
  JSON.stringify([identity.hostId, identity.sessionId, identity.requestId]);

const parseIdentity = (data: unknown): ApprovalNotificationIdentity | null => {
  if (typeof data !== 'object' || data === null) {
    return null;
  }

  const { host_id: hostId, session_id: sessionId, request_id: requestId } =
    data as Record<string, unknown>;
  if (
    typeof hostId !== 'string' || hostId.trim().length === 0 ||
    typeof sessionId !== 'string' || sessionId.trim().length === 0 ||
    typeof requestId !== 'string' || requestId.trim().length === 0
  ) {
    return null;
  }
  return { hostId, sessionId, requestId };
};

const loadResolvedKeys = async (): Promise<Set<string>> => {
  if (resolvedKeys !== null) {
    return resolvedKeys;
  }

  resolvedKeys = new Set();
  try {
    const stored = await AsyncStorage.getItem(resolvedStorageKey);
    const parsed: unknown = stored === null ? [] : JSON.parse(stored);
    if (Array.isArray(parsed)) {
      resolvedKeys = new Set(
        parsed.filter((key): key is string => typeof key === 'string')
          .slice(-maxTrackedApprovals),
      );
    }
  } catch {
    console.warn('Could not load resolved approval notifications');
  }
  return resolvedKeys;
};

export const dismissApprovalNotifications = async (
  identity: ApprovalNotificationIdentity,
): Promise<void> => {
  if (Platform.OS !== 'ios' && Platform.OS !== 'android') {
    return;
  }

  await enqueue(async () => {
    const key = identityKey(identity);
    const resolved = await loadResolvedKeys();
    resolved.delete(key);
    resolved.add(key);
    if (resolved.size > maxTrackedApprovals) {
      resolved.delete(resolved.values().next().value!);
    }
    try {
      await AsyncStorage.setItem(resolvedStorageKey, JSON.stringify([...resolved]));
    } catch {
      console.warn('Could not save resolved approval notifications');
    }

    const identifiers = new Set(deliveredIdentifiers.get(key));
    // The OS inventory also covers notifications delivered before this JS runtime.
    const delivered = await Notifications.getPresentedNotificationsAsync();
    for (const notification of delivered) {
      const data = notification.request.content.data ?? {};
      const candidate = parseIdentity(data);
      if (
        data.kind === 'approval_requested' &&
        candidate !== null && identityKey(candidate) === key
      ) {
        identifiers.add(notification.request.identifier);
      }
    }

    await Promise.all([...identifiers].map(Notifications.dismissNotificationAsync));
    deliveredIdentifiers.delete(key);
  }).catch(() => {
    console.warn('Could not dismiss resolved approval notifications');
  });
};

export const handleApprovalResolutionData = async (data: unknown): Promise<boolean> => {
  if (
    typeof data !== 'object' || data === null ||
    !('kind' in data) || data.kind !== 'approval_resolved'
  ) {
    return false;
  }
  const identity = parseIdentity(data);
  if (identity === null) {
    return false;
  }
  await dismissApprovalNotifications(identity);
  return true;
};

export const trackApprovalNotification = async (
  notification: Notifications.Notification,
): Promise<void> => {
  const data = notification.request.content.data ?? {};
  if (await handleApprovalResolutionData(data)) {
    return;
  }
  const identity = parseIdentity(data);
  if (data.kind !== 'approval_requested' || identity === null) {
    return;
  }

  await enqueue(async () => {
    const key = identityKey(identity);
    const resolved = await loadResolvedKeys();
    if (resolved.has(key)) {
      // Resolution and request pushes can arrive out of order.
      await Notifications.dismissNotificationAsync(notification.request.identifier);
      return;
    }
    const identifiers = deliveredIdentifiers.get(key) ?? new Set<string>();
    identifiers.add(notification.request.identifier);
    deliveredIdentifiers.set(key, identifiers);
    if (deliveredIdentifiers.size > maxTrackedApprovals) {
      deliveredIdentifiers.delete(deliveredIdentifiers.keys().next().value!);
    }
  }).catch(() => {
    console.warn('Could not track approval notification');
  });
};
