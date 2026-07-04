import Constants from 'expo-constants';
import * as Notifications from 'expo-notifications';
import * as SecureStore from 'expo-secure-store';
import { Platform } from 'react-native';

import { registerDevice, unregisterDevice } from '@/lib/hub/client';

const deviceIDKey = 'tethon.push-device-id';
const lastTokenKey = 'tethon.push-last-token';
const androidChannelID = 'approvals';

type RegistrationResult =
  | { status: 'registered'; token: string }
  | { status: 'skipped'; reason: string };

export const registerCurrentDevice = async (): Promise<RegistrationResult> => {
  if (Platform.OS !== 'ios' && Platform.OS !== 'android') {
    return { status: 'skipped', reason: 'unsupported-platform' };
  }

  if (Platform.OS === 'android') {
    await Notifications.setNotificationChannelAsync(androidChannelID, {
      name: 'Approvals',
      importance: Notifications.AndroidImportance.MAX,
    });
  }

  const { status: existingStatus } = await Notifications.getPermissionsAsync();
  const finalStatus =
    existingStatus === 'granted'
      ? existingStatus
      : (await Notifications.requestPermissionsAsync()).status;

  if (finalStatus !== 'granted') {
    return { status: 'skipped', reason: 'permission-not-granted' };
  }

  const projectId =
    Constants.expoConfig?.extra?.eas?.projectId ?? Constants.easConfig?.projectId;
  if (typeof projectId !== 'string' || projectId.length === 0) {
    return { status: 'skipped', reason: 'missing-project-id' };
  }

  const token = (await Notifications.getExpoPushTokenAsync({ projectId })).data;
  const deviceId = await getOrCreateDeviceID();
  const appID = Constants.expoConfig?.slug ?? Constants.expoConfig?.name ?? '';
  const appVersion = Constants.expoConfig?.version ?? '';

  await registerDevice({
    device_id: deviceId,
    push_service: 'expo',
    push_token: token,
    platform: Platform.OS,
    app_id: appID,
    app_version: appVersion,
  });
  await SecureStore.setItemAsync(lastTokenKey, token);

  return { status: 'registered', token };
};

export const unregisterCurrentDevice = async (): Promise<void> => {
  const deviceId = await SecureStore.getItemAsync(deviceIDKey);
  if (deviceId === null) {
    return;
  }

  await unregisterDevice({ deviceId });
  await SecureStore.deleteItemAsync(lastTokenKey);
};

const getOrCreateDeviceID = async (): Promise<string> => {
  const existing = await SecureStore.getItemAsync(deviceIDKey);
  if (existing !== null) {
    return existing;
  }

  const generated = randomID();
  await SecureStore.setItemAsync(deviceIDKey, generated);
  return generated;
};

const randomID = (): string => {
  const randomUUID = globalThis.crypto?.randomUUID;
  if (typeof randomUUID === 'function') {
    return randomUUID.call(globalThis.crypto);
  }

  const random = Math.random().toString(36).slice(2, 12);
  return `dev_${Date.now().toString(36)}_${random}`;
};
