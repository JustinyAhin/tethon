import { useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';

import { SessionEventCard } from '@/components/session-event-card';
import { StatusPill } from '@/components/status-pill';
import {
  cancelSession,
  deleteSession,
  HubAuthError,
  HubRequestError,
} from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import {
  EventApprovalResolved,
  type ApprovalResolvedPayload,
  type SessionEvent,
  type SessionSummary,
} from '@/lib/hub/types';
import { useSessionStream } from '@/lib/hub/use-session-stream';
import { useSessions } from '@/lib/hub/use-sessions';

export default function SessionDetail() {
  const { host, local } = useLocalSearchParams<{
    host: string;
    local: string;
  }>();
  const hostId = Array.isArray(host) ? host[0] : host;
  const localId = Array.isArray(local) ? local[0] : local;
  const id = `${hostId}/${localId}`;
  const { error, loading, refresh, sessions } = useSessions();
  const session = useMemo(
    () => sessions.find((candidate) => candidate.id === id),
    [id, sessions],
  );

  if (loading) {
    return (
      <SafeAreaView style={styles.safeArea}>
        <View style={styles.centerState}>
          <ActivityIndicator />
        </View>
      </SafeAreaView>
    );
  }

  if (error !== null && session === undefined) {
    return (
      <SafeAreaView style={styles.safeArea}>
        <View style={styles.centerState}>
          <Text style={styles.error}>{error}</Text>
        </View>
      </SafeAreaView>
    );
  }

  if (session === undefined) {
    return (
      <SafeAreaView style={styles.safeArea}>
        <View style={styles.centerState}>
          <Text style={styles.emptyText}>Session not found.</Text>
        </View>
      </SafeAreaView>
    );
  }

  return (
    <SessionDetailContent
      hostId={hostId}
      key={id}
      listError={error}
      localId={localId}
      refresh={refresh}
      session={session}
    />
  );
}

const SessionDetailContent = ({
  hostId,
  listError,
  localId,
  refresh,
  session,
}: {
  hostId: string;
  listError: string | null;
  localId: string;
  refresh: () => Promise<void>;
  session: SessionSummary;
}) => {
  const router = useRouter();
  const { invalidate } = useAuth();
  const [busyAction, setBusyAction] = useState<'stop' | 'delete' | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const { events, status: streamStatus, streamError } = useSessionStream({
    hostId,
    localId,
  });
  const listRef = useRef<FlatList<SessionEvent>>(null);
  const atBottomRef = useRef(true);
  const isActive =
    session.status === 'running' || session.status === 'awaiting_approval';
  const resolvedApprovals = useMemo(() => {
    const resolved: Record<string, ApprovalResolvedPayload> = {};

    for (const event of events) {
      if (event.type !== EventApprovalResolved) {
        continue;
      }

      const payload = event.payload as ApprovalResolvedPayload;
      resolved[payload.request_id] = payload;
    }

    return resolved;
  }, [events]);

  const stop = async () => {
    setBusyAction('stop');
    setActionError(null);

    try {
      await cancelSession({ hostId, localId });
      await refresh();
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setActionError(toActionError(caught, 'Could not stop session'));
    } finally {
      setBusyAction(null);
    }
  };

  const deleteCurrentSession = async () => {
    setBusyAction('delete');
    setActionError(null);

    try {
      await deleteSession({ hostId, localId });
      router.back();
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      if (caught instanceof HubRequestError && caught.status === 409) {
        setActionError('Session is still running. Stop it before deleting.');
        return;
      }

      setActionError(toActionError(caught, 'Could not delete session'));
    } finally {
      setBusyAction(null);
    }
  };

  const confirmDelete = () => {
    Alert.alert(
      'Delete session?',
      'This removes the session record from the hub.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Delete',
          style: 'destructive',
          onPress: () => {
            void deleteCurrentSession();
          },
        },
      ],
    );
  };

  const onScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    const {
      contentOffset,
      contentSize,
      layoutMeasurement,
    } = event.nativeEvent;
    const distanceToEnd =
      contentSize.height - (contentOffset.y + layoutMeasurement.height);
    atBottomRef.current = distanceToEnd < 80;
  };

  const scrollToTailIfPinned = () => {
    if (!atBottomRef.current) {
      return;
    }

    requestAnimationFrame(() => {
      listRef.current?.scrollToEnd({ animated: false });
    });
  };

  const header = (
    <View style={styles.headerStack}>
      <View style={styles.headerBlock}>
        <View style={styles.titleRow}>
          <View style={styles.titleBlock}>
            <Text style={styles.title}>
              {session.repo || session.repo_id || session.id}
            </Text>
            <Text style={styles.sessionId}>{session.id}</Text>
          </View>
          <StatusPill status={session.status} />
        </View>

        <View style={styles.meta}>
          <MetaRow label="host" value={`${hostId} · ${session.agent}`} />
          <MetaRow label="branch" value={session.branch || '-'} />
          <MetaRow label="cwd" value={session.cwd || '-'} />
          <MetaRow
            label="created"
            value={new Date(session.created_at).toLocaleString()}
          />
        </View>
      </View>

      {listError !== null ? (
        <View style={styles.errorBanner}>
          <Text style={styles.errorBannerText}>{listError}</Text>
        </View>
      ) : null}

      {actionError !== null ? (
        <View style={styles.errorBanner}>
          <Text style={styles.errorBannerText}>{actionError}</Text>
        </View>
      ) : null}

      {streamError !== null ? (
        <View style={styles.errorBanner}>
          <Text style={styles.errorBannerText}>Stream error: {streamError}</Text>
        </View>
      ) : null}

      <View style={styles.actions}>
        <StreamStatusChip status={streamStatus} />
        {isActive ? (
          <Pressable
            disabled={busyAction !== null}
            onPress={() => {
              void stop();
            }}
            style={({ pressed }) => [
              styles.button,
              styles.stopButton,
              busyAction !== null && styles.buttonDisabled,
              pressed && styles.buttonPressed,
            ]}
          >
            {busyAction === 'stop' ? (
              <ActivityIndicator color="#ffffff" />
            ) : (
              <Text style={styles.stopButtonText}>Stop</Text>
            )}
          </Pressable>
        ) : (
          <Pressable
            disabled={busyAction !== null}
            onPress={confirmDelete}
            style={({ pressed }) => [
              styles.button,
              styles.deleteButton,
              busyAction !== null && styles.buttonDisabled,
              pressed && styles.buttonPressed,
            ]}
          >
            {busyAction === 'delete' ? (
              <ActivityIndicator color="#b42318" />
            ) : (
              <Text style={styles.deleteButtonText}>Delete</Text>
            )}
          </Pressable>
        )}
      </View>
    </View>
  );

  return (
    <SafeAreaView style={styles.safeArea}>
      <View style={styles.container}>
        <FlatList
          ref={listRef}
          contentContainerStyle={styles.listContent}
          data={events}
          initialNumToRender={18}
          keyExtractor={(event) => String(event.seq)}
          ListEmptyComponent={
            <Text style={styles.emptyStreamText}>Waiting for events...</Text>
          }
          ListHeaderComponent={header}
          onContentSizeChange={scrollToTailIfPinned}
          onScroll={onScroll}
          renderItem={({ item }) => (
            <SessionEventCard
              event={item}
              resolvedApprovals={resolvedApprovals}
            />
          )}
          scrollEventThrottle={80}
          showsVerticalScrollIndicator={false}
          style={styles.container}
          windowSize={9}
        />
        {/* tethon-927.4: input bar mounts below the list */}
      </View>
    </SafeAreaView>
  );
};

const StreamStatusChip = ({
  status,
}: {
  status: 'connecting' | 'live' | 'finished' | 'error';
}) => (
  <View
    style={[
      styles.streamChip,
      status === 'live'
        ? styles.streamChipLive
        : status === 'error'
          ? styles.streamChipError
          : styles.streamChipNeutral,
    ]}
  >
    <Text
      style={[
        styles.streamChipText,
        status === 'live'
          ? styles.streamChipLiveText
          : status === 'error'
            ? styles.streamChipErrorText
            : styles.streamChipNeutralText,
      ]}
    >
      {status}
    </Text>
  </View>
);

const MetaRow = ({ label, value }: { label: string; value: string }) => (
  <View style={styles.metaRow}>
    <Text style={styles.metaLabel}>{label}</Text>
    <Text style={styles.metaValue}>{value}</Text>
  </View>
);

const toActionError = (error: unknown, fallback: string): string => {
  if (error instanceof HubRequestError) {
    if (error.status === undefined) {
      return 'Could not reach hub';
    }

    return `Hub returned HTTP ${error.status}`;
  }

  return fallback;
};

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: '#f7f9fb',
  },
  container: {
    flex: 1,
  },
  listContent: {
    gap: 8,
    padding: 20,
  },
  centerState: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  error: {
    color: '#b42318',
    fontSize: 15,
    textAlign: 'center',
  },
  emptyText: {
    color: '#5c6873',
    fontSize: 15,
    textAlign: 'center',
  },
  headerBlock: {
    gap: 16,
  },
  headerStack: {
    gap: 16,
    paddingBottom: 8,
  },
  titleRow: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    gap: 12,
  },
  titleBlock: {
    flex: 1,
    gap: 6,
  },
  title: {
    color: '#101820',
    fontSize: 24,
    fontWeight: '700',
    lineHeight: 30,
  },
  sessionId: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },
  meta: {
    gap: 8,
  },
  metaRow: {
    gap: 3,
  },
  metaLabel: {
    color: '#8a96a3',
    fontSize: 12,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  metaValue: {
    color: '#2f3a43',
    fontSize: 14,
    lineHeight: 20,
  },
  errorBanner: {
    borderColor: '#f3bfbb',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#fbeae9',
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  errorBannerText: {
    color: '#b42318',
    fontSize: 13,
    lineHeight: 18,
  },
  actions: {
    alignItems: 'center',
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 12,
  },
  button: {
    minHeight: 44,
    minWidth: 108,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 8,
    paddingHorizontal: 16,
  },
  stopButton: {
    backgroundColor: '#b42318',
  },
  stopButtonText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '700',
  },
  deleteButton: {
    borderColor: '#b42318',
    borderWidth: 1,
    backgroundColor: '#ffffff',
  },
  deleteButtonText: {
    color: '#b42318',
    fontSize: 15,
    fontWeight: '700',
  },
  buttonDisabled: {
    opacity: 0.55,
  },
  buttonPressed: {
    opacity: 0.8,
  },
  emptyStreamText: {
    color: '#5c6873',
    fontSize: 15,
    paddingVertical: 20,
    textAlign: 'center',
  },
  streamChip: {
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  streamChipLive: {
    backgroundColor: '#e7f6ed',
  },
  streamChipError: {
    backgroundColor: '#fbeae9',
  },
  streamChipNeutral: {
    backgroundColor: '#f1f3f5',
  },
  streamChipText: {
    fontSize: 12,
    fontWeight: '700',
    lineHeight: 16,
  },
  streamChipLiveText: {
    color: '#087443',
  },
  streamChipErrorText: {
    color: '#b42318',
  },
  streamChipNeutralText: {
    color: '#5c6873',
  },
});
