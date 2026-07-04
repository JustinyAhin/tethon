import { useState } from 'react';
import {
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';

import type { ApprovalAction } from '@/lib/hub/types';

type ResolveArgs = {
  action: ApprovalAction;
  requestId: string;
  text?: string;
};

type ApprovalActionsProps = {
  error: string | null;
  requestId: string;
  submitting: boolean;
  onResolve: (args: ResolveArgs) => void;
};

export const ApprovalActions = ({
  error,
  onResolve,
  requestId,
  submitting,
}: ApprovalActionsProps) => {
  const [replyOpen, setReplyOpen] = useState(false);
  const [replyText, setReplyText] = useState('');
  const [localError, setLocalError] = useState<string | null>(null);

  const resolve = (action: ApprovalAction) => {
    setLocalError(null);
    onResolve({ action, requestId });
  };

  const sendReply = () => {
    const trimmed = replyText.trim();

    if (!trimmed) {
      setLocalError('Reply text is required.');
      return;
    }

    setLocalError(null);
    onResolve({ action: 'reply', requestId, text: trimmed });
  };

  const shownError = localError ?? error;

  return (
    <View style={styles.wrap}>
      <View style={styles.row}>
        <Pressable
          disabled={submitting}
          onPress={() => resolve('approve')}
          style={({ pressed }) => [
            styles.button,
            styles.approveButton,
            submitting && styles.disabled,
            pressed && styles.pressed,
          ]}
        >
          <Text style={styles.solidButtonText}>Approve</Text>
        </Pressable>
        <Pressable
          disabled={submitting}
          onPress={() => resolve('deny')}
          style={({ pressed }) => [
            styles.button,
            styles.denyButton,
            submitting && styles.disabled,
            pressed && styles.pressed,
          ]}
        >
          <Text style={styles.solidButtonText}>Deny</Text>
        </Pressable>
        <Pressable
          disabled={submitting}
          onPress={() => {
            setLocalError(null);
            setReplyOpen((current) => !current);
          }}
          style={({ pressed }) => [
            styles.button,
            styles.replyButton,
            submitting && styles.disabled,
            pressed && styles.pressed,
          ]}
        >
          <Text style={styles.replyButtonText}>Reply</Text>
        </Pressable>
      </View>

      {replyOpen ? (
        <View style={styles.replyBox}>
          <TextInput
            multiline
            onChangeText={(value) => {
              setReplyText(value);
              if (localError !== null) {
                setLocalError(null);
              }
            }}
            placeholder="Reply with instructions..."
            style={styles.replyInput}
            value={replyText}
          />
          <Pressable
            disabled={submitting || !replyText.trim()}
            onPress={sendReply}
            style={({ pressed }) => [
              styles.button,
              styles.sendReplyButton,
              (submitting || !replyText.trim()) && styles.disabled,
              pressed && styles.pressed,
            ]}
          >
            <Text style={styles.solidButtonText}>Send reply</Text>
          </Pressable>
        </View>
      ) : null}

      {shownError !== null && shownError.length > 0 ? (
        <Text style={styles.error}>{shownError}</Text>
      ) : null}
    </View>
  );
};

const styles = StyleSheet.create({
  wrap: {
    gap: 8,
  },
  row: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
  },
  button: {
    minHeight: 44,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 8,
    paddingHorizontal: 14,
  },
  approveButton: {
    backgroundColor: '#087443',
  },
  denyButton: {
    backgroundColor: '#b42318',
  },
  replyButton: {
    borderColor: '#ecd9ab',
    borderWidth: 1,
    backgroundColor: '#ffffff',
  },
  sendReplyButton: {
    alignSelf: 'flex-start',
    backgroundColor: '#9a6700',
  },
  solidButtonText: {
    color: '#ffffff',
    fontSize: 14,
    fontWeight: '700',
  },
  replyButtonText: {
    color: '#9a6700',
    fontSize: 14,
    fontWeight: '700',
  },
  replyBox: {
    gap: 8,
  },
  replyInput: {
    maxHeight: 112,
    borderColor: '#ecd9ab',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    color: '#101820',
    fontSize: 14,
    lineHeight: 20,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  disabled: {
    opacity: 0.55,
  },
  pressed: {
    opacity: 0.8,
  },
  error: {
    color: '#b42318',
    fontSize: 12,
    lineHeight: 16,
  },
});
