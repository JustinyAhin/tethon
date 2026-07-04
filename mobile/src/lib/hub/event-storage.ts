import AsyncStorage from '@react-native-async-storage/async-storage';

import type { SessionEvent } from '@/lib/hub/types';

export const maxStoredEvents = 500;

const sessionEventsKey = ({
  hostId,
  localId,
}: {
  hostId: string;
  localId: string;
}) => `tethon:events:${hostId}/${localId}`;

export const loadSessionEvents = async ({
  hostId,
  localId,
}: {
  hostId: string;
  localId: string;
}): Promise<SessionEvent[]> => {
  try {
    const raw = await AsyncStorage.getItem(sessionEventsKey({ hostId, localId }));

    if (raw === null) {
      return [];
    }

    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? (parsed as SessionEvent[]) : [];
  } catch {
    return [];
  }
};

export const saveSessionEvents = async ({
  events,
  hostId,
  localId,
}: {
  events: SessionEvent[];
  hostId: string;
  localId: string;
}): Promise<void> => {
  try {
    await AsyncStorage.setItem(
      sessionEventsKey({ hostId, localId }),
      JSON.stringify(events.slice(-maxStoredEvents)),
    );
  } catch {
    // Best-effort cache only; replay from the hub remains the source of truth.
  }
};

export const clearSessionEvents = async ({
  hostId,
  localId,
}: {
  hostId: string;
  localId: string;
}): Promise<void> => {
  try {
    await AsyncStorage.removeItem(sessionEventsKey({ hostId, localId }));
  } catch {
    // Best-effort cache cleanup.
  }
};
