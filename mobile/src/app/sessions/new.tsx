import { useEffect, useMemo, useState } from 'react';
import Ionicons from '@expo/vector-icons/Ionicons';
import { useRouter } from 'expo-router';
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import {
  createSession as createHubSession,
  fetchHosts,
  fetchRepoRefs,
  HubAuthError,
  HubRequestError,
  sessionPath,
} from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import type {
  CreateSessionRequest,
  HostSummary,
  RepoRefsResponse,
} from '@/lib/hub/types';

type SelectOption = {
  value: string;
  label: string;
  detail?: string;
};

const permissionModes: SelectOption[] = [
  { value: 'default', label: 'default' },
  { value: 'acceptEdits', label: 'acceptEdits' },
  { value: 'auto', label: 'auto' },
  { value: 'bypassPermissions', label: 'bypassPermissions' },
  { value: 'dontAsk', label: 'dontAsk' },
  { value: 'plan', label: 'plan' },
];

export default function NewSession() {
  const router = useRouter();
  const { invalidate, status } = useAuth();
  const [hosts, setHosts] = useState<HostSummary[]>([]);
  const [hostsLoading, setHostsLoading] = useState(true);
  const [hostsError, setHostsError] = useState<string | null>(null);
  const [hostId, setHostId] = useState('');
  const [repoId, setRepoId] = useState('');
  const [useRawRepo, setUseRawRepo] = useState(false);
  const [rawRepo, setRawRepo] = useState('');
  const [agent, setAgent] = useState('');
  const [permissionMode, setPermissionMode] = useState('default');
  const [cwd, setCwd] = useState('');
  const [branch, setBranch] = useState('');
  const [baseRef, setBaseRef] = useState('');
  const [prompt, setPrompt] = useState('');
  const [refs, setRefs] = useState<RepoRefsResponse | null>(null);
  const [refsLoading, setRefsLoading] = useState(false);
  const [refsError, setRefsError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    if (status !== 'signed-in') {
      return;
    }

    let cancelled = false;

    const loadHosts = async () => {
      setHostsLoading(true);
      setHostsError(null);

      try {
        const response = await fetchHosts();

        if (!cancelled) {
          setHosts(response.hosts.filter((host) => host.online));
        }
      } catch (caught) {
        if (cancelled) {
          return;
        }

        if (caught instanceof HubAuthError) {
          await invalidate();
          return;
        }

        setHostsError(toErrorMessage(caught, 'Could not load hosts'));
      } finally {
        if (!cancelled) {
          setHostsLoading(false);
        }
      }
    };

    void loadHosts();

    return () => {
      cancelled = true;
    };
  }, [invalidate, status]);

  const selectedHost = useMemo(
    () => hosts.find((host) => host.host_id === hostId),
    [hostId, hosts],
  );
  const repos = useMemo(() => selectedHost?.repos ?? [], [selectedHost]);
  const rawRepoMode = useRawRepo || repos.length === 0;
  const hostOptions = useMemo(
    () =>
      hosts.map((host) => ({
        value: host.host_id,
        label: host.name,
        detail: host.host_id,
      })),
    [hosts],
  );
  const repoOptions = useMemo(
    () =>
      repos.map((repo) => ({
        value: repo.id,
        label: repo.name,
        detail: repo.path,
      })),
    [repos],
  );
  const agentOptions = useMemo<SelectOption[]>(() => {
    const hostAgents = selectedHost?.agents ?? [];
    return [
      { value: '', label: 'default (fake)' },
      ...hostAgents.map((hostAgent) => ({
        value: hostAgent,
        label: hostAgent,
      })),
    ];
  }, [selectedHost]);
  const refOptions = useMemo<SelectOption[]>(
    () =>
      [
        ...(refs?.branches ?? []).map((ref) => ({
          value: ref,
          label: ref,
          detail: 'branch',
        })),
        ...(refs?.tags ?? []).map((ref) => ({
          value: ref,
          label: ref,
          detail: 'tag',
        })),
      ],
    [refs],
  );

  useEffect(() => {
    if (rawRepoMode || hostId.length === 0 || repoId.length === 0) {
      return;
    }

    let cancelled = false;

    const loadRefs = async () => {
      setRefsLoading(true);
      setRefsError(null);

      try {
        const response = await fetchRepoRefs({ hostId, repoId });

        if (!cancelled) {
          setRefs({
            branches: response.branches ?? [],
            tags: response.tags ?? [],
          });
        }
      } catch (caught) {
        if (cancelled) {
          return;
        }

        if (caught instanceof HubAuthError) {
          await invalidate();
          return;
        }

        setRefsError(toErrorMessage(caught, 'Could not load refs'));
      } finally {
        if (!cancelled) {
          setRefsLoading(false);
        }
      }
    };

    void loadRefs();

    return () => {
      cancelled = true;
    };
  }, [hostId, invalidate, rawRepoMode, repoId]);

  const setSelectedHost = (value: string) => {
    setHostId(value);
    setRepoId('');
    setUseRawRepo(false);
    setRawRepo('');
    setAgent('');
    setPermissionMode('default');
    setBaseRef('');
    setRefs(null);
    setRefsError(null);
    setSubmitError(null);
  };

  const setSelectedRepo = (value: string) => {
    const repo = repos.find((candidate) => candidate.id === value);

    setRepoId(value);
    setRefs(null);
    setRefsError(null);
    setBaseRef(repo?.default_branch ?? '');
  };

  const submit = async () => {
    const cleanHostId = hostId.trim();
    const cleanRepoId = repoId.trim();
    const cleanRawRepo = rawRepo.trim();
    const cleanCwd = cwd.trim();
    const cleanBranch = branch.trim();
    const cleanBaseRef = baseRef.trim();
    const cleanAgent = agent.trim();
    const cleanPrompt = prompt.trim();

    if (cleanHostId.length === 0) {
      setSubmitError('Select a host.');
      return;
    }

    if (rawRepoMode) {
      if (cleanRawRepo.length === 0) {
        setSubmitError('Enter a repo path.');
        return;
      }
    } else if (cleanRepoId.length === 0) {
      setSubmitError('Select a repo.');
      return;
    }

    const body: CreateSessionRequest = {
      host_id: cleanHostId,
      cwd: cleanCwd,
      branch: cleanBranch,
      agent: cleanAgent,
    };

    if (rawRepoMode) {
      body.repo = cleanRawRepo;
    } else {
      body.repo_id = cleanRepoId;
    }

    if (cleanBaseRef.length > 0) {
      body.base_ref = cleanBaseRef;
    }

    if (cleanPrompt.length > 0) {
      body.prompt = cleanPrompt;
    }

    if (cleanAgent === 'claude-cli' && permissionMode.length > 0) {
      body.permission_mode = permissionMode;
    }

    setSubmitting(true);
    setSubmitError(null);

    try {
      const response = await createHubSession(body);
      const { hostId: createdHost, localId } = sessionPath(response.id);

      router.replace({
        pathname: '/sessions/[host]/[local]',
        params: { host: createdHost, local: localId },
      });
    } catch (caught) {
      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setSubmitError(toErrorMessage(caught, 'Could not start session'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <SafeAreaView edges={['bottom']} style={styles.safeArea}>
      <KeyboardAvoidingView
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
        style={styles.keyboard}
      >
        {hostsLoading ? (
          <View style={styles.centerState}>
            <ActivityIndicator />
          </View>
        ) : hostsError !== null ? (
          <View style={styles.centerState}>
            <Text style={styles.errorText}>{hostsError}</Text>
          </View>
        ) : hosts.length === 0 ? (
          <View style={styles.centerState}>
            <Text style={styles.emptyText}>No online hosts.</Text>
          </View>
        ) : (
          <ScrollView
            contentContainerStyle={styles.content}
            keyboardShouldPersistTaps="handled"
          >
            <SelectField
              label="Host"
              onChange={setSelectedHost}
              options={hostOptions}
              placeholder="Select a host"
              value={hostId}
            />

            {selectedHost === undefined ? null : (
              <>
                {rawRepoMode ? (
                  <TextField
                    autoCapitalize="none"
                    label="Repo path"
                    onChangeText={setRawRepo}
                    placeholder="/abs/path/to/repo"
                    value={rawRepo}
                  />
                ) : (
                  <SelectField
                    label="Repo"
                    onChange={setSelectedRepo}
                    options={repoOptions}
                    placeholder="Select a repo"
                    value={repoId}
                  />
                )}

                {repos.length > 0 ? (
                  <View style={styles.toggleRow}>
                    <View style={styles.toggleCopy}>
                      <Text style={styles.toggleLabel}>Raw path</Text>
                      <Text style={styles.toggleDetail}>
                        Use a path instead of a registered repo
                      </Text>
                    </View>
                    <Switch onValueChange={setUseRawRepo} value={useRawRepo} />
                  </View>
                ) : null}

                <SelectField
                  label="Agent"
                  onChange={setAgent}
                  options={agentOptions}
                  value={agent}
                />

                {agent === 'claude-cli' ? (
                  <SelectField
                    label="Permission mode"
                    onChange={setPermissionMode}
                    options={permissionModes}
                    value={permissionMode}
                  />
                ) : null}

                <TextField
                  autoCapitalize="none"
                  label="cwd"
                  onChangeText={setCwd}
                  placeholder="subdir/within/repo"
                  value={cwd}
                />

                <TextField
                  autoCapitalize="none"
                  label="branch"
                  onChangeText={setBranch}
                  value={branch}
                />

                <View style={styles.field}>
                  <Text style={styles.label}>base_ref</Text>
                  <View style={styles.refRow}>
                    <TextInput
                      autoCapitalize="none"
                      onChangeText={setBaseRef}
                      placeholder="branch, tag, or SHA"
                      style={[styles.input, styles.refInput]}
                      value={baseRef}
                    />
                    <SelectField
                      compact
                      disabled={refOptions.length === 0}
                      label="Ref"
                      onChange={setBaseRef}
                      options={refOptions}
                      placeholder="Refs"
                      value={baseRef}
                    />
                  </View>
                  {refsLoading ? (
                    <Text style={styles.helperText}>Loading refs...</Text>
                  ) : refsError !== null ? (
                    <Text style={styles.errorText}>{refsError}</Text>
                  ) : null}
                </View>

                <TextField
                  label="prompt"
                  multiline
                  onChangeText={setPrompt}
                  value={prompt}
                />

                {submitError !== null ? (
                  <View style={styles.errorBanner}>
                    <Text style={styles.errorBannerText}>{submitError}</Text>
                  </View>
                ) : null}

                <Pressable
                  disabled={submitting}
                  onPress={() => {
                    void submit();
                  }}
                  style={({ pressed }) => [
                    styles.submitButton,
                    (pressed || submitting) && styles.pressed,
                  ]}
                >
                  {submitting ? (
                    <ActivityIndicator color="#ffffff" />
                  ) : (
                    <>
                      <Ionicons color="#ffffff" name="play" size={18} />
                      <Text style={styles.submitButtonText}>Start session</Text>
                    </>
                  )}
                </Pressable>
              </>
            )}
          </ScrollView>
        )}
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

const SelectField = ({
  compact = false,
  disabled = false,
  label,
  onChange,
  options,
  placeholder = 'Select',
  value,
}: {
  compact?: boolean;
  disabled?: boolean;
  label: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  value: string;
}) => {
  const [open, setOpen] = useState(false);
  const selected = options.find((option) => option.value === value);

  return (
    <View style={compact ? styles.compactField : styles.field}>
      {compact ? null : <Text style={styles.label}>{label}</Text>}
      <Pressable
        disabled={disabled}
        onPress={() => {
          setOpen(true);
        }}
        style={({ pressed }) => [
          compact ? styles.compactSelect : styles.select,
          disabled && styles.disabledSelect,
          pressed && !disabled && styles.pressed,
        ]}
      >
        <Text
          numberOfLines={1}
          style={[
            compact ? styles.compactSelectText : styles.selectText,
            selected === undefined && styles.placeholderText,
          ]}
        >
          {selected?.label ?? placeholder}
        </Text>
        <Ionicons color="#5c6873" name="chevron-down" size={18} />
      </Pressable>

      <Modal
        animationType="fade"
        onRequestClose={() => {
          setOpen(false);
        }}
        transparent
        visible={open}
      >
        <Pressable
          onPress={() => {
            setOpen(false);
          }}
          style={styles.modalBackdrop}
        >
          <Pressable style={styles.modalCard}>
            <View style={styles.modalHeader}>
              <Text style={styles.modalTitle}>{label}</Text>
              <Pressable
                accessibilityLabel="Close"
                onPress={() => {
                  setOpen(false);
                }}
                style={styles.modalClose}
              >
                <Ionicons color="#2f3a43" name="close" size={20} />
              </Pressable>
            </View>
            <ScrollView style={styles.modalList}>
              {options.map((option, index) => (
                <Pressable
                  key={`${option.value}:${option.detail ?? index}`}
                  onPress={() => {
                    onChange(option.value);
                    setOpen(false);
                  }}
                  style={({ pressed }) => [
                    styles.optionRow,
                    option.value === value && styles.selectedOption,
                    pressed && styles.pressed,
                  ]}
                >
                  <View style={styles.optionCopy}>
                    <Text style={styles.optionLabel}>{option.label}</Text>
                    {option.detail === undefined ? null : (
                      <Text numberOfLines={1} style={styles.optionDetail}>
                        {option.detail}
                      </Text>
                    )}
                  </View>
                  {option.value === value ? (
                    <Ionicons color="#1769aa" name="checkmark" size={20} />
                  ) : null}
                </Pressable>
              ))}
            </ScrollView>
          </Pressable>
        </Pressable>
      </Modal>
    </View>
  );
};

const TextField = ({
  label,
  multiline = false,
  onChangeText,
  value,
  ...inputProps
}: {
  autoCapitalize?: 'none' | 'sentences' | 'words' | 'characters';
  label: string;
  multiline?: boolean;
  onChangeText: (value: string) => void;
  placeholder?: string;
  value: string;
}) => (
  <View style={styles.field}>
    <Text style={styles.label}>{label}</Text>
    <TextInput
      multiline={multiline}
      onChangeText={onChangeText}
      style={[styles.input, multiline && styles.textArea]}
      textAlignVertical={multiline ? 'top' : 'center'}
      value={value}
      {...inputProps}
    />
  </View>
);

const toErrorMessage = (error: unknown, fallback: string): string => {
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
  keyboard: {
    flex: 1,
  },
  centerState: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  content: {
    gap: 16,
    padding: 20,
    paddingBottom: 36,
  },
  field: {
    gap: 7,
  },
  compactField: {
    width: 98,
  },
  label: {
    color: '#2f3a43',
    fontSize: 14,
    fontWeight: '700',
  },
  input: {
    minHeight: 44,
    borderColor: '#cdd6df',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    color: '#101820',
    fontSize: 15,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  textArea: {
    minHeight: 104,
    lineHeight: 21,
  },
  select: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 10,
    borderColor: '#cdd6df',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    paddingHorizontal: 12,
  },
  compactSelect: {
    minHeight: 44,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 4,
    borderColor: '#cdd6df',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    paddingHorizontal: 10,
  },
  disabledSelect: {
    opacity: 0.45,
  },
  selectText: {
    flex: 1,
    color: '#101820',
    fontSize: 15,
  },
  compactSelectText: {
    color: '#2f3a43',
    fontSize: 14,
    fontWeight: '700',
  },
  placeholderText: {
    color: '#8a96a3',
  },
  pressed: {
    opacity: 0.75,
  },
  toggleRow: {
    minHeight: 56,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 16,
    borderColor: '#dce3ea',
    borderRadius: 8,
    borderWidth: 1,
    backgroundColor: '#ffffff',
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  toggleCopy: {
    flex: 1,
    gap: 3,
  },
  toggleLabel: {
    color: '#101820',
    fontSize: 14,
    fontWeight: '700',
  },
  toggleDetail: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },
  refRow: {
    flexDirection: 'row',
    gap: 8,
  },
  refInput: {
    flex: 1,
  },
  helperText: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },
  errorText: {
    color: '#b42318',
    fontSize: 14,
    lineHeight: 20,
    textAlign: 'center',
  },
  emptyText: {
    color: '#5c6873',
    fontSize: 15,
    lineHeight: 21,
    textAlign: 'center',
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
  submitButton: {
    minHeight: 46,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    borderRadius: 8,
    backgroundColor: '#1769aa',
    paddingHorizontal: 14,
  },
  submitButtonText: {
    color: '#ffffff',
    fontSize: 15,
    fontWeight: '700',
  },
  modalBackdrop: {
    flex: 1,
    justifyContent: 'flex-end',
    backgroundColor: 'rgba(16, 24, 32, 0.38)',
  },
  modalCard: {
    maxHeight: '76%',
    borderTopLeftRadius: 12,
    borderTopRightRadius: 12,
    backgroundColor: '#ffffff',
    paddingBottom: 24,
  },
  modalHeader: {
    minHeight: 56,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    borderBottomColor: '#edf1f5',
    borderBottomWidth: 1,
    paddingHorizontal: 18,
  },
  modalTitle: {
    color: '#101820',
    fontSize: 17,
    fontWeight: '700',
  },
  modalClose: {
    width: 36,
    height: 36,
    alignItems: 'center',
    justifyContent: 'center',
  },
  modalList: {
    maxHeight: 420,
  },
  optionRow: {
    minHeight: 54,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
    borderBottomColor: '#edf1f5',
    borderBottomWidth: 1,
    paddingHorizontal: 18,
    paddingVertical: 10,
  },
  selectedOption: {
    backgroundColor: '#eef6fc',
  },
  optionCopy: {
    flex: 1,
    gap: 3,
  },
  optionLabel: {
    color: '#101820',
    fontSize: 15,
    fontWeight: '600',
  },
  optionDetail: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },
});
