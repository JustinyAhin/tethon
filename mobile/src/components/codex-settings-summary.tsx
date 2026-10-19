import { useEffect, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import {
  fetchCodexSettings,
  HubAuthError,
  HubRequestError,
} from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import type { CodexSettingsResponse } from '@/lib/hub/types';

const CodexSettingsSummary = ({
  hostId,
  cwd,
  onSettings,
}: {
  hostId: string;
  cwd?: string;
  onSettings?: (settings: CodexSettingsResponse | null) => void;
}) => {
  const { invalidate } = useAuth();
  const [settings, setSettings] = useState<CodexSettingsResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    let cancelled = false;

    void fetchCodexSettings({ hostId, cwd })
      .then((response) => {
        if (!cancelled) {
          setSettings(response);
          setError(null);
          onSettings?.(response);
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
          caught instanceof HubRequestError
            ? caught.message
            : 'Could not read host settings.',
        );
        onSettings?.(null);
      });

    return () => {
      cancelled = true;
    };
  }, [cwd, hostId, invalidate, onSettings, reload]);

  return (
    <View style={styles.wrap}>
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
        onPress={() => setOpen((current) => !current)}
        style={styles.toggle}
      >
        <Text style={styles.title}>Codex on this host</Text>
        <Text style={styles.body}>{open ? 'Hide' : 'Show'} settings</Text>
      </Pressable>

      {error ? (
        <Text accessibilityRole="alert" style={styles.error}>
          {error}
        </Text>
      ) : settings ? (
        <>
          <Text style={styles.body}>
            Account: {settings.account.status}
            {settings.account.plan_type
              ? ` · ${settings.account.plan_type}`
              : ''}
          </Text>

          {settings.account.login_command ? (
            <Text selectable style={styles.body}>
              Run {settings.account.login_command} on the host to sign in.
            </Text>
          ) : null}

          {settings.rate_limits.ordinary_usage_allowed === false ? (
            <Text style={styles.error}>
              Account usage is blocked. Check the account limits on the host.
            </Text>
          ) : null}

          {open ? (
            <>
              <Text style={styles.detail}>Working folder: {settings.cwd}</Text>
              <Text style={styles.body}>
                Permission policy:{' '}
                {settings.defaults.approval_policy || 'Host default'}
              </Text>

              <Text style={styles.body}>
                File access: {settings.defaults.sandbox_mode || 'Host default'}
              </Text>
              {settings.managed.configured ? (
                <Text style={styles.detail}>
                  The host has managed requirements. These requirements can
                  limit overrides.
                </Text>
              ) : null}
              <Text style={styles.body}>
                Configuration: {settings.config_status}
              </Text>
              {settings.rate_limits.buckets.map((bucket, index) => (
                <Text key={`${bucket.id}:${index}`} style={styles.detail}>
                  {bucket.name || bucket.id || 'Usage'}
                  {bucket.primary
                    ? ` · ${Math.round(bucket.primary.used_percent)}% used`
                    : ''}
                  {bucket.primary?.resets_at
                    ? ` · Resets ${new Date(bucket.primary.resets_at * 1000).toLocaleString()}`
                    : ''}
                </Text>
              ))}

              <Text style={styles.title}>
                Skills · {settings.skills.status}
              </Text>
              {settings.skills.skills
                .filter((skill) => skill.enabled)
                .map((skill) => (
                  <Text
                    key={`${skill.scope}:${skill.name}`}
                    style={styles.body}
                  >
                    {skill.display_name || skill.name}
                    {skill.description ? ` · ${skill.description}` : ''}
                  </Text>
                ))}

              <Text style={styles.title}>Tools · {settings.mcp.status}</Text>
              {settings.mcp.servers.map((server) => (
                <Text key={server.name} style={styles.body}>
                  {server.name} · {server.auth_status}
                  {server.runtime_status ? ` · ${server.runtime_status}` : ''}
                  {server.tools.length ? ` · ${server.tools.join(', ')}` : ''}
                </Text>
              ))}

              <Text style={styles.title}>
                Collaboration · {settings.collaboration.status}
              </Text>
              {settings.collaboration.modes.map((mode) => (
                <Text key={mode.mode} style={styles.body}>
                  {mode.name}
                </Text>
              ))}

              <Text style={styles.detail}>
                New conversations use the host settings unless you select an
                override.
              </Text>
            </>
          ) : null}

          {settings.warnings.map((warning, index) => (
            <Text key={index} style={styles.detail}>
              {warning}
            </Text>
          ))}
        </>
      ) : (
        <Text style={styles.detail}>Reading account and host settings…</Text>
      )}

      {error || open ? (
        <Pressable
          accessibilityRole="button"
          onPress={() => setReload((value) => value + 1)}
          style={styles.toggle}
        >
          <Text style={styles.body}>Refresh host settings</Text>
        </Pressable>
      ) : null}
    </View>
  );
};

const styles = StyleSheet.create({
  wrap: {
    padding: 12,
    gap: 8,
    borderWidth: 1,
    borderColor: '#dce3ea',
    borderRadius: 8,
    backgroundColor: '#fff',
  },

  toggle: { minHeight: 44, justifyContent: 'center', gap: 4 },

  title: { fontSize: 14, fontWeight: '700', color: '#101820' },

  body: { fontSize: 13, lineHeight: 19, color: '#2f3a43' },

  detail: { fontSize: 12, lineHeight: 18, color: '#5c6873' },

  error: { fontSize: 13, lineHeight: 19, color: '#b42318' },
});

export { CodexSettingsSummary };
