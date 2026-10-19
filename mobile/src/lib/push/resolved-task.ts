import * as Notifications from 'expo-notifications';
import * as TaskManager from 'expo-task-manager';
import { Platform } from 'react-native';

import {
  handleApprovalResolutionData,
  trackApprovalNotification,
} from '@/lib/push/resolved';

const taskName = 'tethon-approval-resolution';

if (Platform.OS === 'ios' || Platform.OS === 'android') {
  TaskManager.defineTask<Notifications.NotificationTaskPayload>(taskName, async ({ data, error }) => {
    if (error) {
      return Notifications.BackgroundNotificationTaskResult.Failed;
    }
    // Android also invokes tasks for action responses. responses.ts owns those.
    if ('actionIdentifier' in data) {
      return Notifications.BackgroundNotificationTaskResult.NoData;
    }

    let payload: unknown = data.data;
    if (typeof data.data.dataString === 'string') {
      try {
        payload = JSON.parse(data.data.dataString);
      } catch {
        return Notifications.BackgroundNotificationTaskResult.NoData;
      }
    }
    const handled = await handleApprovalResolutionData(payload);
    return handled
      ? Notifications.BackgroundNotificationTaskResult.NewData
      : Notifications.BackgroundNotificationTaskResult.NoData;
  });

  Notifications.addNotificationReceivedListener((notification) => {
    void trackApprovalNotification(notification);
  });

  void Notifications.registerTaskAsync(taskName).catch(() => {
    console.warn('Could not register approval resolution notifications');
  });
}
