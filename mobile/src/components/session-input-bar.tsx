import { useState } from 'react';
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

type SessionInputBarProps = {
  hostId: string;
  localId: string;
  onFocusInput?: () => void;
};

export const SessionInputBar = ({
  hostId,
  localId,
  onFocusInput,
}: SessionInputBarProps) => {
  const { invalidate } = useAuth();
  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState<string | null>(null);

  const send = async () => {
    if (!text.trim() || sending) {
      return;
    }

    setSending(true);
    setSendError(null);

    try {
      await sendInput({ hostId, localId, text });
      setText('');
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setSendError(toSendError(caught));
    } finally {
      setSending(false);
    }
  };

  const disabled = !text.trim() || sending;

  return (
    <View style={styles.wrap}>
      <View style={styles.row}>
        <TextInput
          autoCapitalize="sentences"
          multiline
          onChangeText={setText}
          onFocus={onFocusInput}
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
            <Text style={styles.buttonText}>Send</Text>
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
      return 'Session is no longer active.';
    }

    if (error.status === 400) {
      return 'Message is empty.';
    }

    if (error.status === 404) {
      return 'Session not found.';
    }

    if (error.status === undefined) {
      return 'Could not reach hub.';
    }
  }

  return 'Could not send message.';
};

const styles = StyleSheet.create({
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
