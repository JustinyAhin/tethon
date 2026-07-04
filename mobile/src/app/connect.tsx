import { useState } from 'react';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { HubAuthError, HubRequestError } from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';

export default function Connect() {
  const { hubUrl, signIn } = useAuth();
  const [url, setUrl] = useState(hubUrl ?? '');
  const [token, setToken] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const connect = async () => {
    setPending(true);
    setError(null);

    try {
      await signIn({ url, token });
    } catch (caught) {
      setError(toConnectErrorMessage(caught));
    } finally {
      setPending(false);
    }
  };

  return (
    <SafeAreaView style={styles.safeArea}>
      <KeyboardAvoidingView
        behavior={Platform.select({ ios: 'padding', default: undefined })}
        style={styles.container}
      >
        <View style={styles.content}>
          <View style={styles.header}>
            <Text style={styles.title}>Connect to tethon</Text>
            <Text style={styles.subtitle}>Enter the hub URL and access token.</Text>
          </View>

          <View style={styles.form}>
            <View style={styles.field}>
              <Text style={styles.label}>Hub URL</Text>
              <TextInput
                autoCapitalize="none"
                autoCorrect={false}
                keyboardType="url"
                onChangeText={setUrl}
                placeholder="http://127.0.0.1:39180"
                style={styles.input}
                value={url}
              />
            </View>

            <View style={styles.field}>
              <Text style={styles.label}>Token</Text>
              <TextInput
                autoCapitalize="none"
                autoCorrect={false}
                onChangeText={setToken}
                placeholder="Optional"
                secureTextEntry
                style={styles.input}
                value={token}
              />
            </View>

            {error !== null ? <Text style={styles.error}>{error}</Text> : null}

            <Pressable
              disabled={pending || url.trim().length === 0}
              onPress={connect}
              style={({ pressed }) => [
                styles.button,
                (pending || url.trim().length === 0) && styles.buttonDisabled,
                pressed && styles.buttonPressed,
              ]}
            >
              {pending ? (
                <ActivityIndicator color="#ffffff" />
              ) : (
                <Text style={styles.buttonText}>Connect</Text>
              )}
            </Pressable>
          </View>
        </View>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

const toConnectErrorMessage = (error: unknown): string => {
  if (error instanceof HubAuthError) {
    return 'Token rejected by hub';
  }

  if (error instanceof HubRequestError) {
    if (error.status === undefined) {
      return 'Could not reach hub - check the URL';
    }

    return `Hub returned an error (HTTP ${error.status})`;
  }

  if (error instanceof TypeError) {
    return 'Enter a valid hub URL';
  }

  return 'Could not connect to hub';
};

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
    backgroundColor: '#f7f9fb',
  },
  container: {
    flex: 1,
  },
  content: {
    flex: 1,
    justifyContent: 'center',
    padding: 24,
  },
  header: {
    gap: 8,
    marginBottom: 32,
  },
  title: {
    color: '#101820',
    fontSize: 28,
    fontWeight: '700',
  },
  subtitle: {
    color: '#5c6873',
    fontSize: 16,
    lineHeight: 22,
  },
  form: {
    gap: 18,
  },
  field: {
    gap: 8,
  },
  label: {
    color: '#2f3a43',
    fontSize: 14,
    fontWeight: '600',
  },
  input: {
    minHeight: 50,
    borderColor: '#cdd6df',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    color: '#101820',
    fontSize: 16,
    paddingHorizontal: 14,
  },
  error: {
    color: '#b42318',
    fontSize: 14,
    lineHeight: 20,
  },
  button: {
    minHeight: 52,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: 8,
    backgroundColor: '#1769aa',
  },
  buttonDisabled: {
    opacity: 0.55,
  },
  buttonPressed: {
    opacity: 0.8,
  },
  buttonText: {
    color: '#ffffff',
    fontSize: 16,
    fontWeight: '700',
  },
});
