import { useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';

import { StatusPill } from '@/components/status-pill';
import {
  cancelSession,
  deleteSession,
  HubAuthError,
  HubRequestError,
} from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import { useSessions } from '@/lib/hub/use-sessions';

export default function SessionDetail() {
  const router = useRouter();
  const { invalidate } = useAuth();
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
  const [busyAction, setBusyAction] = useState<'stop' | 'delete' | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

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

  const isActive =
    session.status === 'running' || session.status === 'awaiting_approval';

  return (
    <SafeAreaView style={styles.safeArea}>
      <ScrollView
        contentContainerStyle={styles.scrollContent}
        style={styles.container}
      >
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

        {error !== null ? (
          <View style={styles.errorBanner}>
            <Text style={styles.errorBannerText}>{error}</Text>
          </View>
        ) : null}

        {actionError !== null ? (
          <View style={styles.errorBanner}>
            <Text style={styles.errorBannerText}>{actionError}</Text>
          </View>
        ) : null}

        <View style={styles.actions}>
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

        {/* tethon-927.3: stream mounts here; 927.4 input bar below */}
        <View style={styles.streamSurface}>
          <Text style={styles.streamText}>Event stream coming in a later update</Text>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}

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
  scrollContent: {
    flexGrow: 1,
    gap: 16,
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
    flexDirection: 'row',
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
  streamSurface: {
    flex: 1,
    minHeight: 260,
    alignItems: 'center',
    justifyContent: 'center',
    borderColor: '#dce3ea',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    padding: 20,
  },
  streamText: {
    color: '#5c6873',
    fontSize: 15,
    textAlign: 'center',
  },
});
