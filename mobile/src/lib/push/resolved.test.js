import { beforeEach, describe, expect, mock, test } from 'bun:test';

let presented = [];
let dismissed = [];
const stored = new Map();
const tasks = new Map();

mock.module('@react-native-async-storage/async-storage', () => ({
  default: {
    getItem: async (key) => stored.get(key) ?? null,
    setItem: async (key, value) => {
      stored.set(key, value);
    },
  },
}));

mock.module('react-native', () => ({ Platform: { OS: 'ios' } }));

mock.module('expo-notifications', () => ({
  getPresentedNotificationsAsync: async () => presented,
  dismissNotificationAsync: async (identifier) => {
    dismissed.push(identifier);
  },
  addNotificationReceivedListener: () => ({ remove() {} }),
  registerTaskAsync: async () => null,
  BackgroundNotificationTaskResult: { NewData: 0, NoData: 1, Failed: 2 },
}));

mock.module('expo-task-manager', () => ({
  defineTask: (name, task) => {
    tasks.set(name, task);
  },
}));

const { dismissApprovalNotifications, trackApprovalNotification } =
  await import('./resolved');
await import('./resolved-task');

const notification = (identifier, hostId, sessionId, requestId) => ({
  request: {
    identifier,
    content: {
      data: {
        kind: 'approval_requested',
        host_id: hostId,
        session_id: sessionId,
        request_id: requestId,
      },
    },
  },
});

beforeEach(() => {
  presented = [];
  dismissed = [];
});

describe('approval notification resolution', () => {
  test('uses the full identity and OS inventory after a cold start', async () => {
    presented = [
      notification('matched', 'host-a', 'session-a', 'request-a'),
      notification('other-host', 'host-b', 'session-a', 'request-a'),
      notification('other-session', 'host-a', 'session-b', 'request-a'),
      notification('other-request', 'host-a', 'session-a', 'request-b'),
    ];

    await dismissApprovalNotifications({
      hostId: 'host-a',
      sessionId: 'session-a',
      requestId: 'request-a',
    });

    expect(dismissed).toEqual(['matched']);
  });

  test('removes a known delivery even when OS inventory has not updated', async () => {
    await trackApprovalNotification(
      notification('known', 'host-c', 'session-c', 'request-c'),
    );

    await dismissApprovalNotifications({
      hostId: 'host-c',
      sessionId: 'session-c',
      requestId: 'request-c',
    });

    expect(dismissed).toEqual(['known']);
  });

  test('removes a late request push after its resolution', async () => {
    await dismissApprovalNotifications({
      hostId: 'host-d',
      sessionId: 'session-d',
      requestId: 'request-d',
    });

    await trackApprovalNotification(
      notification('late', 'host-d', 'session-d', 'request-d'),
    );

    expect(dismissed).toEqual(['late']);
    expect(stored.size).toBe(1);
  });

  test('handles Android JSON data and iOS direct data in the background task', async () => {
    const task = tasks.get('tethon-approval-resolution');

    presented = [notification('android', 'host-e', 'session-e', 'request-e')];
    const payload = {
      kind: 'approval_resolved',
      host_id: 'host-e',
      session_id: 'session-e',
      request_id: 'request-e',
    };

    expect(
      await task({
        data: { data: { dataString: JSON.stringify(payload) } },
        error: null,
      }),
    ).toBe(0);
    expect(dismissed).toEqual(['android']);

    presented = [notification('ios', 'host-f', 'session-f', 'request-f')];

    expect(
      await task({
        data: {
          data: {
            ...payload,
            host_id: 'host-f',
            session_id: 'session-f',
            request_id: 'request-f',
          },
        },
      }),
    ).toBe(0);
    expect(dismissed).toEqual(['android', 'ios']);

    expect(
      await task({
        data: { actionIdentifier: 'approval_approve' },
        error: null,
      }),
    ).toBe(1);
    expect(
      await task({ data: { data: { dataString: '{invalid' } }, error: null }),
    ).toBe(1);
  });
});
