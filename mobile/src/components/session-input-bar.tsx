import { useEffect, useRef, useState } from 'react';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import { useAuth } from '@/lib/hub/auth-context';
import { HubAuthError, HubRequestError, sendInput } from '@/lib/hub/client';
import type { CodexSessionState } from '@/lib/hub/types';
import { newClientRequestId } from '@/lib/hub/creation-intents';
import {
  NativeTurnOptions,
  type NativeTurnSelection,
} from '@/components/native-turn-options';

type PendingInput = {
  key: string;
  id: string;
  text?: string;
  mode?: 'start' | 'steer' | 'queue';
  turnId?: string;
  options?: NativeTurnSelection;
};

type ComposerDraft = {
  version: 1;
  text: string;
  pending: PendingInput | null;
  options?: NativeTurnSelection;
};

let pendingDraftWrite: Promise<unknown> = Promise.resolve();

const parseTurnSelection = (value: unknown): NativeTurnSelection => {
  if (typeof value !== 'object' || value === null) {
    return {};
  }

  const candidate = value as Record<string, unknown>;

  return {
    requestedModel:
      typeof candidate.requestedModel === 'string'
        ? candidate.requestedModel
        : undefined,
    reasoningEffort:
      typeof candidate.reasoningEffort === 'string'
        ? candidate.reasoningEffort
        : undefined,
    permissionProfile:
      typeof candidate.permissionProfile === 'string'
        ? candidate.permissionProfile
        : undefined,
  };
};

const saveComposerDraft = (
  key: string,
  draft: ComposerDraft,
): Promise<void> => {
  const operation = pendingDraftWrite.then(() =>
    AsyncStorage.setItem(key, JSON.stringify(draft)),
  );

  pendingDraftWrite = operation.catch(() => undefined);
  return operation;
};

const parseComposerDraft = (raw: string): ComposerDraft => {
  try {
    const value: unknown = JSON.parse(raw);

    if (
      typeof value === 'object' &&
      value !== null &&
      'version' in value &&
      value.version === 1 &&
      'text' in value &&
      typeof value.text === 'string'
    ) {
      const stored = value as ComposerDraft;
      const pending = stored.pending;

      return {
        version: 1,
        text: value.text,
        options: parseTurnSelection(stored.options),
        pending:
          pending !== null &&
          typeof pending === 'object' &&
          typeof pending.key === 'string' &&
          typeof pending.id === 'string' &&
          typeof pending.text === 'string' &&
          ['start', 'steer', 'queue'].includes(pending.mode ?? '') &&
          (pending.turnId === undefined || typeof pending.turnId === 'string')
            ? {
                ...pending,
                options:
                  pending.options === undefined
                    ? undefined
                    : parseTurnSelection(pending.options),
              }
            : null,
      };
    }
  } catch {
    // Earlier drafts contain plain text.
  }

  return { version: 1, text: raw, pending: null };
};

type SessionInputBarProps = {
  hostId: string;
  localId: string;
  onFocusInput?: () => void;
  native?: boolean;
  disabled?: boolean;
  codexState?: CodexSessionState;
  queueAvailable?: boolean;
  cwd?: string;
};

const SessionInputBar = ({
  hostId,
  localId,
  onFocusInput,
  native = false,
  disabled: unavailable = false,
  codexState,
  queueAvailable = false,
  cwd,
}: SessionInputBarProps) => {
  const { invalidate, hubUrl } = useAuth();

  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);
  const [mode, setMode] = useState<'steer' | 'queue'>('steer');
  const [loadedDraftKey, setLoadedDraftKey] = useState<string | null>(null);
  const [pendingInput, setPendingInput] = useState<PendingInput | null>(null);
  const [options, setOptions] = useState<NativeTurnSelection>({});
  const pendingSend = useRef<PendingInput | null>(null);
  const latestDraft = useRef<ComposerDraft | null>(null);

  const draftKey = `tethon:draft:${JSON.stringify([hubUrl, hostId, localId])}`;
  const draftLoaded = loadedDraftKey === draftKey;
  const activeTurn = codexState?.turn_state === 'in_progress';

  useEffect(() => {
    let cancelled = false;

    void AsyncStorage.getItem(draftKey)
      .then((stored) => {
        if (!cancelled) {
          if (stored !== null) {
            const draft = parseComposerDraft(stored);
            setText(draft.text);
            pendingSend.current = draft.pending;
            setPendingInput(draft.pending);
            setOptions(draft.options ?? {});

            if (
              draft.pending?.mode === 'queue' ||
              draft.pending?.mode === 'steer'
            ) {
              setMode(draft.pending.mode);
            }

            latestDraft.current = draft;
          }

          setLoadedDraftKey(draftKey);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setLoadedDraftKey(draftKey);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [draftKey]);

  useEffect(() => {
    if (!draftLoaded) {
      return;
    }

    const draft: ComposerDraft = {
      version: 1,
      text,
      pending: pendingInput,
      options,
    };
    latestDraft.current = draft;

    const timer = setTimeout(() => {
      if (latestDraft.current !== null) {
        void saveComposerDraft(draftKey, latestDraft.current).catch(() => {});
      }
    }, 250);

    return () => clearTimeout(timer);
  }, [draftKey, draftLoaded, pendingInput, options, text]);

  useEffect(
    () => () => {
      if (latestDraft.current !== null) {
        void saveComposerDraft(draftKey, latestDraft.current).catch(() => {});
      }
    },
    [draftKey],
  );

  const changeText = (next: string) => {
    if (next !== text) {
      pendingSend.current = null;
      setPendingInput(null);
    }

    latestDraft.current = {
      version: 1,
      text: next,
      pending: pendingSend.current,
      options,
    };
    setText(next);
  };

  const changeMode = (next: 'steer' | 'queue') => {
    pendingSend.current = null;
    setPendingInput(null);
    setMode(next);

    latestDraft.current = { version: 1, text, pending: null, options };
  };

  const changeOptions = (next: NativeTurnSelection) => {
    pendingSend.current = null;
    setPendingInput(null);
    setOptions(next);

    latestDraft.current = { version: 1, text, pending: null, options: next };
  };

  const send = async () => {
    if (
      !text.trim() ||
      sending ||
      !draftLoaded ||
      unavailable ||
      (native && activeTurn && mode === 'queue' && !queueAvailable)
    ) {
      return;
    }

    setSending(true);
    setSendError(null);

    try {
      const saved = pendingSend.current;
      // A retry keeps the original turn and options after reconnect or restart.
      const retrySaved =
        native && saved?.text === text && saved.mode !== undefined;
      const inputMode = retrySaved
        ? saved.mode
        : native
          ? activeTurn
            ? mode
            : 'start'
          : undefined;
      const turnId = retrySaved
        ? saved.turnId
        : native && activeTurn
          ? codexState?.turn_id
          : undefined;
      const turnOptions = retrySaved
        ? saved.options
        : inputMode === 'start'
          ? options
          : undefined;
      const key = JSON.stringify([text, inputMode, turnId, turnOptions]);

      const submission = retrySaved
        ? saved
        : {
            key,
            id: newClientRequestId(),
            text,
            mode: inputMode,
            turnId,
            options: turnOptions,
          };

      pendingSend.current = submission;
      setPendingInput(submission);

      if (native) {
        const draft: ComposerDraft = {
          version: 1,
          text,
          pending: submission,
          options,
        };

        // Store the UUID before transport so a lost response cannot create a new action.
        latestDraft.current = draft;
        await saveComposerDraft(draftKey, draft);
      }

      await sendInput({
        hostId,
        localId,
        text,
        mode: inputMode,
        turnId,
        requestId: native ? submission.id : undefined,
        requestedModel: turnOptions?.requestedModel,
        reasoningEffort: turnOptions?.reasoningEffort,
        permissionProfile: turnOptions?.permissionProfile,
      });

      setText('');
      pendingSend.current = null;
      setPendingInput(null);
      latestDraft.current = { version: 1, text: '', pending: null, options };

      void saveComposerDraft(draftKey, latestDraft.current).catch(() => {});

      if (inputMode === 'queue') {
        setSendError('Message sent to the queue. Waiting for host state.');
      }
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setSendError(
        native &&
          caught instanceof HubRequestError &&
          caught.status === undefined
          ? 'Delivery is not confirmed. Check the conversation before you retry.'
          : toSendError(caught),
      );
    } finally {
      setSending(false);
    }
  };

  const disabled =
    !text.trim() ||
    sending ||
    !draftLoaded ||
    unavailable ||
    (native && activeTurn && mode === 'queue' && !queueAvailable);

  return (
    <View style={styles.wrap}>
      {native && !activeTurn ? (
        <NativeTurnOptions
          hostId={hostId}
          cwd={cwd}
          value={options}
          disabled={sending || unavailable || !cwd}
          onChange={changeOptions}
        />
      ) : null}

      {native && activeTurn ? (
        <View style={styles.row}>
          {(['steer', 'queue'] as const).map((value) => (
            <Pressable
              key={value}
              disabled={
                sending || unavailable || (value === 'queue' && !queueAvailable)
              }
              onPress={() => changeMode(value)}
              style={[styles.modeButton, mode === value && styles.modeSelected]}
            >
              <Text>
                {value === 'steer'
                  ? 'Send to active turn'
                  : 'Queue next message'}
              </Text>
            </Pressable>
          ))}
        </View>
      ) : null}

      {native && pendingInput?.text === text && !sending ? (
        <Text style={styles.errorText}>
          A saved action has no confirmed result. Retry keeps its original
          target and request.
        </Text>
      ) : null}

      {native && unavailable ? (
        <Text style={styles.errorText}>
          Wait for the host connection before sending.
        </Text>
      ) : null}

      <View style={styles.row}>
        <TextInput
          autoCapitalize="sentences"
          multiline
          onChangeText={changeText}
          onFocus={onFocusInput}
          editable={!unavailable && draftLoaded}
          placeholder="Send a message..."
          style={styles.input}
          value={text}
        />
        <Pressable
          disabled={disabled}
          onPress={() => {
            void send();
          }}
          style={({ pressed }) => [
            styles.button,
            disabled && styles.buttonDisabled,
            pressed && styles.buttonPressed,
          ]}
        >
          {sending ? (
            <ActivityIndicator color="#ffffff" />
          ) : (
            <Text style={styles.buttonText}>
              {native && pendingInput?.text === text
                ? 'Retry'
                : native && activeTurn && mode === 'queue'
                  ? 'Queue'
                  : 'Send'}
            </Text>
          )}
        </Pressable>
      </View>

      {sendError !== null ? (
        <Text style={styles.errorText}>{sendError}</Text>
      ) : null}
    </View>
  );
};

const toSendError = (error: unknown): string => {
  if (error instanceof HubRequestError) {
    if (error.status === 409) {
      return error.message;
    }

    if (error.status === 400) {
      return error.message;
    }

    if (error.status === 404) {
      return 'Session not found.';
    }

    if (error.status === undefined) {
      return 'Could not reach hub.';
    }
    return error.message;
  }

  return 'Could not send message.';
};

const styles = StyleSheet.create({
  modeButton: {
    minHeight: 44,
    padding: 8,
    borderWidth: 1,
    borderColor: '#dce3ea',
    borderRadius: 8,
    justifyContent: 'center',
    flex: 1,
  },

  modeSelected: { backgroundColor: '#e8f1fa', borderColor: '#83a8c8' },

  wrap: {
    gap: 6,
    borderTopColor: '#dce3ea',
    borderTopWidth: 1,
    backgroundColor: '#ffffff',
    paddingHorizontal: 20,
    paddingVertical: 10,
  },

  row: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    gap: 8,
  },

  input: {
    minHeight: 44,
    maxHeight: 112,
    flex: 1,
    borderColor: '#cdd6df',
    borderRadius: 8,
    borderWidth: 1,
    color: '#101820',
    fontSize: 15,
    lineHeight: 20,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },

  button: {
    minHeight: 44,
    minWidth: 76,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 8,
    backgroundColor: '#1769aa',
    paddingHorizontal: 16,
  },

  buttonDisabled: {
    opacity: 0.55,
  },

  buttonPressed: {
    opacity: 0.8,
  },

  buttonText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '700',
  },

  errorText: {
    color: '#b42318',
    fontSize: 12,
    lineHeight: 16,
  },
});

export { SessionInputBar };
