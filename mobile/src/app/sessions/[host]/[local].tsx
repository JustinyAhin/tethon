import { useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  KeyboardAvoidingView,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useHeaderHeight } from 'expo-router/react-navigation';
import { SafeAreaView } from 'react-native-safe-area-context';

import { SessionInputBar } from '@/components/session-input-bar';
import { nativeSessionLabel } from '@/lib/hub/session-status';
import { CodexTimelineCard } from '@/components/codex-timeline-card';
import { CodexRequestCard } from '@/components/codex-request-card';
import { CodexSettingsSummary } from '@/components/codex-settings-summary';
import { SessionEventCard } from '@/components/session-event-card';
import { StatusPill } from '@/components/status-pill';
import {
  cancelSession,
  deleteSession,
  HubAuthError,
  HubRequestError,
  resolveApproval,
  changeNativeConversation,
  type NativeSessionAction,
} from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import { clearSessionEvents } from '@/lib/hub/event-storage';
import {
  EventApprovalResolved,
  type ApprovalAction,
  type ApprovalResolvedPayload,
  type SessionEvent,
  type SessionSummary,
  type CodexRequestPayload,
} from '@/lib/hub/types';
import {
  buildCodexTimeline,
  record,
  list,
  text,
  type TimelineEntry,
} from '@/lib/hub/codex-timeline';
import { requestIdentity, useCodexSession } from '@/lib/hub/use-codex-session';
import { useSessionStream } from '@/lib/hub/use-session-stream';
import { useSessions } from '@/lib/hub/use-sessions';
import { queueNativeApprovalResponse } from '@/lib/push/responses';

export default function SessionDetail() {
  const { host, local, requestKey, connectionId } = useLocalSearchParams<{
    host: string;
    local: string;
    requestKey?: string;
    connectionId?: string;
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
      notificationRequestKey={
        Array.isArray(requestKey) ? requestKey[0] : requestKey
      }
      notificationConnectionId={
        Array.isArray(connectionId) ? connectionId[0] : connectionId
      }
    />
  );
}

const SessionDetailContent = ({
  hostId,
  listError,
  localId,
  refresh,
  session,
  notificationRequestKey,
  notificationConnectionId,
}: {
  hostId: string;
  listError: string | null;
  localId: string;
  refresh: () => Promise<void>;
  session: SessionSummary;
  notificationRequestKey?: string;
  notificationConnectionId?: string;
}) => {
  const router = useRouter();
  const headerHeight = useHeaderHeight();
  const { invalidate } = useAuth();

  const [busyAction, setBusyAction] = useState<
    'stop' | NativeSessionAction | null
  >(null);
  const [showConversationActions, setShowConversationActions] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [nativeSubmitting, setNativeSubmitting] = useState<
    Record<string, boolean>
  >({});
  const [nativeErrors, setNativeErrors] = useState<Record<string, string>>({});
  const nativeInFlight = useRef(new Set<string>());

  const [approvalSubmitting, setApprovalSubmitting] = useState<
    Record<string, boolean>
  >({});
  const [approvalErrors, setApprovalErrors] = useState<Record<string, string>>(
    {},
  );
  const listRef = useRef<FlatList<SessionEvent | TimelineEntry>>(null);
  const atBottomRef = useRef(true);

  const isCodex = session.agent === 'codex-cli' || session.codex !== undefined;

  const {
    events,
    status: streamStatus,
    streamError,
  } = useSessionStream({
    hostId,
    localId,
    persistent: isCodex,
  });
  const native = useCodexSession({
    hostId,
    localId,
    initialState: session.codex,
    events,
    enabled: isCodex,
    streamStatus,
  });
  const workingCwd =
    native.state?.cwd ||
    (native.workspaceOwned
      ? undefined
      : session.cwd?.startsWith('/')
        ? session.cwd
        : [session.repo?.replace(/\/$/, ''), session.cwd]
            .filter(Boolean)
            .join('/') || undefined);

  const nativeEntries = useMemo(
    () =>
      buildCodexTimeline({
        events,
        turns: native.turns,
        currentTurnId: native.state?.turn_id,
      }),
    [events, native.turns, native.state?.turn_id],
  );
  const notificationTarget =
    notificationRequestKey && notificationConnectionId
      ? requestIdentity({
          request_key: notificationRequestKey,
          connection_id: notificationConnectionId,
        })
      : null;
  const nativeRequests = useMemo(
    () =>
      [...native.requests].sort(
        (left, right) =>
          Number(requestIdentity(left) === notificationTarget) -
          Number(requestIdentity(right) === notificationTarget),
      ),
    [native.requests, notificationTarget],
  );

  useEffect(() => {
    if (notificationTarget === null) {
      return;
    }

    atBottomRef.current = true;
    requestAnimationFrame(() => {
      listRef.current?.scrollToEnd({ animated: false });
    });
  }, [notificationTarget]);

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
      await cancelSession({
        hostId,
        localId,
        turnId: isCodex ? native.state?.turn_id : undefined,
      });

      if (isCodex) {
        await native.refresh();
      }

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
      await clearSessionEvents({ hostId, localId });
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

  const changeConversation = async (action: NativeSessionAction) => {
    setBusyAction(action);
    setActionError(null);

    try {
      await changeNativeConversation({ hostId, localId, action });

      if (action === 'delete') {
        await clearSessionEvents({ hostId, localId });
        router.back();
        return;
      }

      await native.refresh();
      await refresh();
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setActionError(
        toActionError(caught, 'Could not change this conversation.'),
      );
    } finally {
      setBusyAction(null);
    }
  };

  const confirmNativeAction = (action: NativeSessionAction) => {
    const title =
      action === 'archive'
        ? 'Archive conversation?'
        : action === 'unarchive'
          ? 'Restore conversation?'
          : action === 'cleanup'
            ? 'Remove isolated working files?'
            : 'Delete Codex conversation?';
    const message =
      action === 'cleanup'
        ? 'The host will remove this Tethon-owned worktree and branch only when they are clean. This action is separate from the conversation.'
        : action === 'delete'
          ? 'This removes the native Codex conversation. Working files remain in place.'
          : action === 'unarchive'
            ? 'Restore this conversation without sending a new message.'
            : 'Archive this conversation and keep its working files.';
    const label =
      action === 'unarchive'
        ? 'Restore'
        : action === 'cleanup'
          ? 'Remove worktree'
          : action === 'archive'
            ? 'Archive'
            : 'Delete';

    Alert.alert(title, message, [
      { text: 'Cancel', style: 'cancel' },
      {
        text: label,
        style:
          action === 'delete' || action === 'cleanup'
            ? 'destructive'
            : 'default',
        onPress: () => {
          void changeConversation(action);
        },
      },
    ]);
  };

  const onScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
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

  const submitApproval = async ({
    action,
    requestId,
    text,
  }: {
    action: ApprovalAction;
    requestId: string;
    text?: string;
  }) => {
    if (approvalSubmitting[requestId] || resolvedApprovals[requestId]) {
      return;
    }

    setApprovalSubmitting((current) => ({ ...current, [requestId]: true }));
    setApprovalErrors((current) => {
      const next = { ...current };
      delete next[requestId];
      return next;
    });

    try {
      await resolveApproval({ action, hostId, localId, requestId, text });
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      if (caught instanceof HubRequestError && caught.status === 404) {
        return;
      }

      setApprovalSubmitting((current) => ({
        ...current,
        [requestId]: false,
      }));
      setApprovalErrors((current) => ({
        ...current,
        [requestId]:
          caught instanceof HubRequestError && caught.status === 409
            ? 'Session is no longer active.'
            : toActionError(caught, 'Could not resolve approval'),
      }));
    }
  };

  const submitNativeRequest = async (
    request: CodexRequestPayload,
    result: unknown,
  ) => {
    const key = requestIdentity(request);

    if (
      !native.ready ||
      native.state?.connection_id !== request.connection_id ||
      nativeInFlight.current.has(key) ||
      nativeSubmitting[key]
    ) {
      return;
    }

    nativeInFlight.current.add(key);
    setNativeSubmitting((current) => ({ ...current, [key]: true }));
    setNativeErrors((current) => ({ ...current, [key]: '' }));

    try {
      await queueNativeApprovalResponse({
        hostId,
        sessionId: localId,
        requestKey: request.request_key,
        connectionId: request.connection_id,
        result,
      });
      await native.refresh();
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setNativeSubmitting((current) => ({ ...current, [key]: false }));
      setNativeErrors((current) => ({
        ...current,
        [key]:
          caught instanceof HubRequestError &&
          (caught.status === 404 || caught.status === 409)
            ? 'This request changed or was answered on another device. Refresh the conversation.'
            : toActionError(caught, 'Could not send this answer.'),
      }));
      if (
        caught instanceof HubRequestError &&
        (caught.status === 404 || caught.status === 409)
      )
        await native.refresh();
    } finally {
      nativeInFlight.current.delete(key);
    }
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
          <StatusPill
            status={isCodex ? nativeSessionLabel(native.state) : session.status}
          />
        </View>

        <View style={styles.meta}>
          <MetaRow label="host" value={`${hostId} · ${session.agent}`} />
          <MetaRow label="branch" value={session.branch || '-'} />
          <MetaRow
            label="cwd"
            value={(isCodex ? workingCwd : session.cwd) || '-'}
          />
          {isCodex ? (
            <>
              <MetaRow
                label="Connection"
                value={native.state?.connection_state ?? 'connecting'}
              />
              <MetaRow
                label="Turn"
                value={nativeTurnLabel(native.state?.turn_state)}
              />
              <MetaRow
                label="Model"
                value={
                  session.model || session.requested_model || 'Host default'
                }
              />
            </>
          ) : null}
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
          <Text style={styles.errorBannerText}>
            Stream error: {streamError}
          </Text>
        </View>
      ) : null}
      {isCodex && native.error ? (
        <View style={styles.errorBanner}>
          <Text style={styles.errorBannerText}>{native.error}</Text>
          <Pressable
            accessibilityRole="button"
            onPress={() => {
              void native.refresh();
            }}
            style={styles.button}
          >
            <Text>Retry connection</Text>
          </Pressable>
        </View>
      ) : null}
      {isCodex && workingCwd ? (
        <CodexSettingsSummary hostId={hostId} cwd={workingCwd} />
      ) : null}
      {isCodex && native.queue.length > 0 ? (
        <View style={styles.headerStack}>
          <Text style={styles.metaLabel}>
            Queued messages · {native.queue.length}
          </Text>
          {native.queue.map((value, index) => {
            const queued = record(value);
            return (
              <Text
                key={text(queued.id) || String(index)}
                style={styles.metaValue}
              >
                {list(queued.input)
                  .map((input) => text(record(input).text))
                  .filter(Boolean)
                  .join('\n') || 'Queued message'}
              </Text>
            );
          })}
        </View>
      ) : null}

      <View style={styles.actions}>
        <StreamStatusChip status={streamStatus} />
        {(isCodex ? native.state?.turn_state === 'in_progress' : isActive) ? (
          <Pressable
            disabled={busyAction !== null || (isCodex && !native.ready)}
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
        ) : !isCodex ? (
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
        ) : null}
        {isCodex && native.hasOlder ? (
          <Pressable
            accessibilityRole="button"
            onPress={() => {
              atBottomRef.current = false;
              void native.loadOlder();
            }}
            style={styles.button}
          >
            <Text>Load earlier messages</Text>
          </Pressable>
        ) : null}
      </View>
      {isCodex ? (
        <View style={styles.headerStack}>
          <Pressable
            accessibilityRole="button"
            accessibilityState={{ expanded: showConversationActions }}
            onPress={() => setShowConversationActions((value) => !value)}
            style={styles.button}
          >
            <Text>Conversation actions</Text>
          </Pressable>
          {showConversationActions ? (
            <View style={styles.actions}>
              {native.state?.conversation_state === 'ready' ? (
                <Pressable
                  accessibilityRole="button"
                  disabled={
                    busyAction !== null ||
                    !native.reconciled ||
                    native.state.turn_state === 'in_progress'
                  }
                  onPress={() => confirmNativeAction('archive')}
                  style={styles.button}
                >
                  <Text>Archive</Text>
                </Pressable>
              ) : null}
              {native.state?.conversation_state === 'archived' ? (
                <Pressable
                  accessibilityRole="button"
                  disabled={busyAction !== null || !native.reconciled}
                  onPress={() => confirmNativeAction('unarchive')}
                  style={styles.button}
                >
                  <Text>Restore</Text>
                </Pressable>
              ) : null}
              <Pressable
                accessibilityRole="button"
                disabled={
                  busyAction !== null ||
                  !native.reconciled ||
                  native.state?.turn_state === 'in_progress'
                }
                onPress={() => confirmNativeAction('delete')}
                style={styles.button}
              >
                <Text style={styles.deleteButtonText}>Delete conversation</Text>
              </Pressable>
              {native.workspaceOwned &&
              native.state?.turn_state !== 'in_progress' &&
              ['archived', 'unavailable'].includes(
                native.state?.conversation_state ?? '',
              ) ? (
                <Pressable
                  accessibilityRole="button"
                  disabled={busyAction !== null || !native.reconciled}
                  onPress={() => confirmNativeAction('cleanup')}
                  style={styles.button}
                >
                  <Text style={styles.deleteButtonText}>
                    Remove isolated worktree
                  </Text>
                </Pressable>
              ) : null}
            </View>
          ) : null}
        </View>
      ) : null}
    </View>
  );

  return (
    <SafeAreaView style={styles.safeArea}>
      <KeyboardAvoidingView
        behavior={Platform.select({ ios: 'padding', default: undefined })}
        keyboardVerticalOffset={headerHeight}
        style={styles.container}
      >
        <FlatList
          ref={listRef}
          contentContainerStyle={styles.listContent}
          data={isCodex ? nativeEntries : events}
          initialNumToRender={18}
          keyboardDismissMode="interactive"
          keyboardShouldPersistTaps="handled"
          keyExtractor={(event) =>
            'seq' in event ? String(event.seq) : event.id
          }
          maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
          ListEmptyComponent={
            <Text style={styles.emptyStreamText}>
              {isCodex
                ? 'Start a message when the host is connected.'
                : 'Waiting for events...'}
            </Text>
          }
          ListHeaderComponent={header}
          ListFooterComponent={
            isCodex ? (
              <View style={styles.headerStack}>
                {nativeRequests.map((request) => (
                  <CodexRequestCard
                    key={requestIdentity(request)}
                    request={request}
                    context={nativeEntries.find(
                      (entry) =>
                        entry.id ===
                        `${request.turn_id ?? ''}:${request.item_id ?? ''}`,
                    )}
                    fromNotification={
                      requestIdentity(request) === notificationTarget
                    }
                    disabled={
                      !native.ready ||
                      native.state?.connection_id !== request.connection_id
                    }
                    busy={nativeSubmitting[requestIdentity(request)] ?? false}
                    error={nativeErrors[requestIdentity(request)]}
                    onRespond={(result) => {
                      void submitNativeRequest(request, result);
                    }}
                  />
                ))}
              </View>
            ) : null
          }
          onContentSizeChange={scrollToTailIfPinned}
          onScroll={onScroll}
          renderItem={({ item }) =>
            !('seq' in item) ? (
              <CodexTimelineCard entry={item} />
            ) : (
              <SessionEventCard
                approvalActions={{
                  errors: approvalErrors,
                  onResolve: (args) => {
                    void submitApproval(args);
                  },
                  submitting: approvalSubmitting,
                }}
                event={item}
                resolvedApprovals={resolvedApprovals}
              />
            )
          }
          scrollEventThrottle={80}
          showsVerticalScrollIndicator={false}
          style={styles.container}
          windowSize={9}
        />
        {streamStatus === 'finished' && !isCodex ? (
          <Text style={styles.finishedText}>Session finished.</Text>
        ) : (
          <SessionInputBar
            hostId={hostId}
            localId={localId}
            onFocusInput={scrollToTailIfPinned}
            native={isCodex}
            disabled={isCodex && !native.ready}
            codexState={native.state}
            queueAvailable={native.queueAvailable}
            cwd={workingCwd}
          />
        )}
      </KeyboardAvoidingView>
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

const nativeTurnLabel = (state?: string): string => {
  switch (state) {
    case 'in_progress':
      return 'Working';
    case 'interrupted':
      return 'Stopped';
    case 'failed':
      return 'Failed';
    case 'completed':
    case 'idle':
      return 'Ready for a message';
    default:
      return 'Waiting for host state';
  }
};

const toActionError = (error: unknown, fallback: string): string => {
  if (error instanceof HubRequestError) {
    if (error.status === undefined) {
      return 'Could not reach hub';
    }

    return error.message;
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

  finishedText: {
    borderTopColor: '#dce3ea',
    borderTopWidth: 1,
    backgroundColor: '#ffffff',
    color: '#5c6873',
    fontSize: 13,
    paddingHorizontal: 20,
    paddingVertical: 16,
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
