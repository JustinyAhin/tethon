import { useEffect, useState } from 'react';
import {
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';

import { useAuth } from '@/lib/hub/auth-context';
import {
  fetchCodexSettings,
  fetchModelCatalog,
  HubAuthError,
} from '@/lib/hub/client';
import type { CodexSettingsResponse, ModelCatalog } from '@/lib/hub/types';

type NativeTurnSelection = {
  requestedModel?: string;
  reasoningEffort?: string;
  permissionProfile?: string;
};

type Choice = { id: string; description?: string };

const NativeTurnOptions = ({
  hostId,
  cwd,
  value,
  disabled,
  onChange,
}: {
  hostId: string;
  cwd?: string;
  value: NativeTurnSelection;
  disabled: boolean;
  onChange: (value: NativeTurnSelection) => void;
}) => {
  const { invalidate } = useAuth();
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [catalog, setCatalog] = useState<ModelCatalog | null>(null);
  const [settings, setSettings] = useState<CodexSettingsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  const model = catalog?.models.find(
    (item) => item.id === value.requestedModel,
  );
  const selected = [
    value.requestedModel,
    value.reasoningEffort,
    value.permissionProfile,
  ]
    .filter(Boolean)
    .join(' · ');

  useEffect(() => {
    if (!open) {
      return;
    }

    let cancelled = false;

    void Promise.all([
      fetchModelCatalog({ hostId, agent: 'codex-cli' }),
      fetchCodexSettings({ hostId, cwd }),
    ])
      .then(([nextCatalog, nextSettings]) => {
        if (!cancelled) {
          setCatalog(nextCatalog);
          setSettings(nextSettings);
          setError(null);
        }
      })
      .catch(async (caught: unknown) => {
        if (cancelled) {
          return;
        }

        if (caught instanceof HubAuthError) {
          await invalidate();
          return;
        }

        setError(
          'Could not read conversation options. Check the host connection.',
        );
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [cwd, hostId, invalidate, open]);

  return (
    <>
      <Pressable
        disabled={disabled}
        onPress={() => {
          setCatalog(null);
          setSettings(null);
          setError(null);
          setLoading(true);
          setOpen(true);
        }}
        style={styles.toggle}
      >
        <Text style={styles.link}>
          Conversation options · {selected || 'Host default'}
        </Text>
      </Pressable>

      <Modal
        visible={open}
        animationType="slide"
        presentationStyle="pageSheet"
        onRequestClose={() => setOpen(false)}
      >
        <ScrollView contentContainerStyle={styles.content}>
          <Text style={styles.title}>Next message options</Text>
          <Text style={styles.body}>
            These options apply when you start the next turn.
          </Text>

          {error ? <Text style={styles.error}>{error}</Text> : null}

          {loading ? (
            <Text style={styles.body}>Reading host choices…</Text>
          ) : null}

          <ChoiceGroup
            label="Model"
            value={value.requestedModel}
            options={catalog?.status === 'ready' ? catalog.models : []}
            onChange={(requestedModel) =>
              onChange({ ...value, requestedModel, reasoningEffort: undefined })
            }
          />

          {model?.reasoning_efforts?.length ? (
            <ChoiceGroup
              label="Reasoning"
              value={value.reasoningEffort}
              options={model.reasoning_efforts}
              onChange={(reasoningEffort) =>
                onChange({ ...value, reasoningEffort })
              }
            />
          ) : null}

          <ChoiceGroup
            label="Permissions"
            value={value.permissionProfile}
            options={(settings?.permission_profiles.profiles ?? []).filter(
              (profile) => profile.allowed,
            )}
            onChange={(permissionProfile) =>
              onChange({ ...value, permissionProfile })
            }
          />

          {settings?.permission_profiles.status === 'unavailable' ? (
            <Text style={styles.body}>
              Permission profiles are unavailable on this host.
            </Text>
          ) : null}

          <Pressable onPress={() => setOpen(false)} style={styles.choice}>
            <Text style={styles.link}>Done</Text>
          </Pressable>
        </ScrollView>
      </Modal>
    </>
  );
};

const ChoiceGroup = ({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value?: string;
  options: Choice[];
  onChange: (value: string | undefined) => void;
}) => (
  <View style={styles.section}>
    <Text style={styles.title}>{label}</Text>
    <Pressable onPress={() => onChange(undefined)} style={styles.choice}>
      <Text style={styles.body}>{!value ? '✓ ' : ''}Host default</Text>
    </Pressable>

    {value && !options.some((option) => option.id === value) ? (
      <Text style={styles.error}>
        Saved choice {value} is not confirmed. Select the host default or a
        current option.
      </Text>
    ) : null}

    {options.map((option) => (
      <Pressable
        key={option.id}
        onPress={() => onChange(option.id)}
        style={styles.choice}
      >
        <Text style={styles.body}>
          {value === option.id ? '✓ ' : ''}
          {option.id}
        </Text>
        {option.description ? (
          <Text style={styles.detail}>{option.description}</Text>
        ) : null}
      </Pressable>
    ))}
  </View>
);

const styles = StyleSheet.create({
  toggle: { minHeight: 44, justifyContent: 'center' },

  content: { padding: 20, paddingBottom: 48, gap: 20 },

  section: { gap: 8 },

  title: { fontSize: 17, fontWeight: '700', color: '#101820' },

  body: { fontSize: 14, lineHeight: 21, color: '#2f3a43' },

  detail: { fontSize: 12, lineHeight: 18, color: '#5c6873' },

  link: { fontSize: 13, color: '#1f4f75' },

  error: { fontSize: 13, lineHeight: 19, color: '#b42318' },

  choice: {
    minHeight: 44,
    justifyContent: 'center',
    padding: 10,
    borderWidth: 1,
    borderColor: '#dce3ea',
    borderRadius: 7,
  },
});

export { NativeTurnOptions, type NativeTurnSelection };
