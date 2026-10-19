import { useEffect, useMemo, useRef, useState } from 'react';
import Ionicons from '@expo/vector-icons/Ionicons';
import { useRouter } from 'expo-router';
import {
  ActivityIndicator,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';

import { CodexSettingsSummary } from '@/components/codex-settings-summary';
import {
  loadCreationDraft,
  saveCreationDraft,
  clearCreationDraft,
  getCreationRequestId,
  clearCreationIntent,
  type CreationDraft,
} from '@/lib/hub/creation-intents';

import {
  createSession as createHubSession,
  fetchHosts,
  fetchRepoRefs,
  fetchModelCatalog,
  fetchNativeConversations,
  type NativeConversation,
  HubAuthError,
  HubRequestError,
  sessionPath,
} from '@/lib/hub/client';
import { useAuth } from '@/lib/hub/auth-context';
import type {
  CreateSessionRequest,
  HostSummary,
  RepoRefsResponse,
  ModelCatalog,
  CodexSettingsResponse,
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
  const { invalidate, status, hubUrl } = useAuth();

  const [hosts, setHosts] = useState<HostSummary[]>([]);
  const [hostsLoading, setHostsLoading] = useState(true);
  const [hostsError, setHostsError] = useState<string | null>(null);
  const [hostId, setHostId] = useState('');
  const [repoId, setRepoId] = useState('');
  const [useRawRepo, setUseRawRepo] = useState(false);
  const [rawRepo, setRawRepo] = useState('');
  const [agent, setAgent] = useState('');

  const [catalog, setCatalog] = useState<ModelCatalog | null>(null);
  const [catalogLoading, setCatalogLoading] = useState(false);
  const [catalogError, setCatalogError] = useState<string | null>(null);
  const [model, setModel] = useState('');
  const [reasoning, setReasoning] = useState('');
  const [permissionProfile, setPermissionProfile] = useState('');
  const [hostSettings, setHostSettings] =
    useState<CodexSettingsResponse | null>(null);

  const [workspaceMode, setWorkspaceMode] = useState('existing');
  const [conversations, setConversations] = useState<NativeConversation[]>([]);
  const [conversationSearch, setConversationSearch] = useState('');
  const [nativeThreadId, setNativeThreadId] = useState('');
  const [conversationsLoading, setConversationsLoading] = useState(false);
  const [conversationsError, setConversationsError] = useState<string | null>(
    null,
  );
  const [conversationCursor, setConversationCursor] = useState<
    string | undefined
  >();
  const conversationRevision = useRef(0);

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
  const [restoredHub, setRestoredHub] = useState<string | null>(null);
  const [draftError, setDraftError] = useState<string | null>(null);
  const draftCleared = useRef(false);
  const latestDraft = useRef<{ hubUrl: string; draft: CreationDraft } | null>(
    null,
  );

  const isCodex = agent === 'codex-cli';
  const isResume = isCodex && nativeThreadId.length > 0;
  const selectedModel = catalog?.models.find((item) => item.id === model);
  const draftReady = hubUrl !== null && restoredHub === hubUrl;
  const draft = useMemo<CreationDraft>(
    () => ({
      hostId,
      repoId,
      useRawRepo,
      rawRepo,
      agent,
      permissionMode,
      permissionProfile,
      cwd,
      branch,
      baseRef,
      prompt,
      model,
      reasoning,
      workspaceMode,
      nativeThreadId,
      conversationSearch,
    }),
    [
      hostId,
      repoId,
      useRawRepo,
      rawRepo,
      agent,
      permissionMode,
      permissionProfile,
      cwd,
      branch,
      baseRef,
      prompt,
      model,
      reasoning,
      workspaceMode,
      nativeThreadId,
      conversationSearch,
    ],
  );

  useEffect(() => {
    if (hubUrl === null) {
      return;
    }

    let cancelled = false;

    void loadCreationDraft(hubUrl)
      .then((stored) => {
        if (cancelled) {
          return;
        }

        if (stored !== null) {
          setHostId(stored.hostId);
          setRepoId(stored.repoId);
          setUseRawRepo(stored.useRawRepo);
          setRawRepo(stored.rawRepo);
          setAgent(stored.agent);

          setPermissionMode(stored.permissionMode);
          setPermissionProfile(stored.permissionProfile);
          setCatalogLoading(stored.agent.length > 0);

          setCwd(stored.cwd);
          setBranch(stored.branch);
          setBaseRef(stored.baseRef);
          setPrompt(stored.prompt);
          setModel(stored.model);
          setReasoning(stored.reasoning);
          setWorkspaceMode(stored.workspaceMode);
          setNativeThreadId(stored.nativeThreadId);
          setConversationSearch(stored.conversationSearch);
        }

        setRestoredHub(hubUrl);
      })
      .catch(() => {
        if (!cancelled) {
          setRestoredHub(hubUrl);
          setDraftError('Could not restore the saved draft.');
        }
      });

    return () => {
      cancelled = true;
    };
  }, [hubUrl]);

  useEffect(() => {
    if (!draftReady || hubUrl === null || !draft.hostId) {
      return;
    }

    draftCleared.current = false;
    latestDraft.current = { hubUrl, draft };

    const timer = setTimeout(() => {
      if (draftCleared.current) {
        return;
      }

      void saveCreationDraft({ hubUrl, draft })
        .then(() => setDraftError(null))
        .catch(() => setDraftError('Could not save this draft.'));
    }, 250);

    return () => clearTimeout(timer);
  }, [draft, draftReady, hubUrl]);

  useEffect(
    () => () => {
      if (latestDraft.current !== null && !draftCleared.current) {
        void saveCreationDraft(latestDraft.current).catch(() => {});
      }
    },
    [],
  );

  useEffect(() => {
    if (!hostId || !agent) {
      return;
    }

    let cancelled = false;

    void fetchModelCatalog({ hostId, agent })
      .then((response) => {
        if (!cancelled) {
          setCatalog(response);
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

        setCatalogError(
          'Models are unavailable. You can use the host default.',
        );
      })
      .finally(() => {
        if (!cancelled) {
          setCatalogLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [agent, hostId, invalidate]);

  useEffect(() => {
    if (!isCodex || !hostId) {
      return;
    }

    conversationRevision.current += 1;
    let cancelled = false;

    const timer = setTimeout(() => {
      setConversations([]);
      setConversationsError(null);
      setConversationCursor(undefined);
      setConversationsLoading(true);

      void fetchNativeConversations({ hostId, search: conversationSearch })
        .then((response) => {
          if (!cancelled) {
            setConversations(response.conversations ?? []);
            setConversationCursor(response.next_cursor);
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

          setConversationsError(
            'Could not load conversations. Check the Codex account and host connection.',
          );
        })
        .finally(() => {
          if (!cancelled) {
            setConversationsLoading(false);
          }
        });
    }, 250);

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [conversationSearch, hostId, invalidate, isCodex]);

  const loadMoreConversations = async () => {
    if (!conversationCursor || conversationsLoading) {
      return;
    }

    setConversationsLoading(true);
    const revision = conversationRevision.current;

    try {
      const response = await fetchNativeConversations({
        hostId,
        search: conversationSearch,
        cursor: conversationCursor,
      });

      if (revision !== conversationRevision.current) {
        return;
      }

      setConversations((current) => [
        ...current,
        ...(response.conversations ?? []).filter(
          (item) =>
            !current.some((existing) => existing.thread_id === item.thread_id),
        ),
      ]);
      setConversationCursor(response.next_cursor);
    } catch (caught) {
      if (revision !== conversationRevision.current) {
        return;
      }

      if (caught instanceof HubAuthError) {
        await invalidate();
        return;
      }

      setConversationsError('Could not load more conversations. Try again.');
    } finally {
      if (revision === conversationRevision.current) {
        setConversationsLoading(false);
      }
    }
  };

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
  const selectedConversation = conversations.find(
    (item) => item.thread_id === nativeThreadId,
  );
  const repoFolder = rawRepoMode
    ? rawRepo.trim()
    : repos.find((item) => item.id === repoId)?.path;
  const workingCwd = isResume
    ? selectedConversation?.cwd
    : repoFolder
      ? [repoFolder.replace(/\/$/, ''), cwd.trim()].filter(Boolean).join('/')
      : undefined;

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
    () => [
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

    setCatalog(null);
    setCatalogError(null);
    setCatalogLoading(false);
    setModel('');
    setReasoning('');
    setPermissionProfile('');
    setHostSettings(null);

    setConversations([]);
    setConversationsError(null);
    setConversationCursor(undefined);
    setNativeThreadId('');
    setConversationSearch('');
    conversationRevision.current += 1;

    setWorkspaceMode('existing');
    setPermissionMode('default');
    setBaseRef('');
    setRefs(null);
    setRefsError(null);
    setSubmitError(null);
  };

  const setSelectedAgent = (value: string) => {
    setAgent(value);

    setCatalog(null);
    setCatalogError(null);
    setCatalogLoading(value.length > 0);
    setModel('');
    setReasoning('');
    setPermissionProfile('');
    setHostSettings(null);

    setNativeThreadId('');
    setWorkspaceMode('existing');
    setConversations([]);
    setConversationsError(null);
    setConversationCursor(undefined);
    conversationRevision.current += 1;
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

    if (isResume && (model || reasoning || permissionProfile) && !cleanPrompt) {
      setSubmitError(
        'Enter a message to apply the selected conversation options.',
      );
      return;
    }

    if (
      permissionProfile &&
      !hostSettings?.permission_profiles.profiles.some(
        (profile) => profile.id === permissionProfile && profile.allowed,
      )
    ) {
      setSubmitError(
        'Confirm an allowed permission profile, or use the host default.',
      );
      return;
    }

    if (
      model &&
      (catalogLoading || catalog?.status !== 'ready' || !selectedModel)
    ) {
      setSubmitError('Confirm the selected model, or select the host default.');
      return;
    }

    if (
      reasoning &&
      !selectedModel?.reasoning_efforts?.some((item) => item.id === reasoning)
    ) {
      setSubmitError(
        'Select a supported reasoning option, or use the host default.',
      );
      return;
    }

    if (cleanHostId.length === 0) {
      setSubmitError('Select a host.');
      return;
    }

    if (!isResume && rawRepoMode) {
      if (cleanRawRepo.length === 0) {
        setSubmitError('Enter a repo path.');
        return;
      }
    } else if (!isResume && cleanRepoId.length === 0) {
      setSubmitError('Select a repo.');
      return;
    }

    const body: CreateSessionRequest = {
      host_id: cleanHostId,
      cwd: isResume ? '' : cleanCwd,
      branch: isCodex && workspaceMode === 'existing' ? '' : cleanBranch,
      agent: cleanAgent,
    };

    if (isResume) {
      body.native_thread_id = nativeThreadId;
    } else if (rawRepoMode) {
      body.repo = cleanRawRepo;
    } else {
      body.repo_id = cleanRepoId;
    }

    if (cleanBaseRef.length > 0 && (!isCodex || workspaceMode === 'isolated')) {
      body.base_ref = cleanBaseRef;
    }

    if (cleanPrompt.length > 0) {
      body.prompt = cleanPrompt;
    }

    if (cleanAgent === 'claude-cli' && permissionMode.length > 0) {
      body.permission_mode = permissionMode;
    }

    if (isCodex && permissionProfile) {
      body.codex_permission_profile = permissionProfile;
    }

    if (model && catalog?.status === 'ready' && selectedModel) {
      body.requested_model = model;
    }

    if (
      reasoning &&
      selectedModel?.reasoning_efforts?.some((item) => item.id === reasoning)
    ) {
      body.reasoning_effort = reasoning;
    }

    if (isCodex && !isResume) {
      body.workspace_mode = workspaceMode;
    }

    setSubmitting(true);
    setSubmitError(null);

    try {
      if (isCodex) {
        if (hubUrl === null) {
          throw new HubAuthError();
        }

        await saveCreationDraft({ hubUrl, draft });
        body.request_id = await getCreationRequestId({
          hubUrl,
          hostId: cleanHostId,
          payload: body,
        });
      }

      const response = await createHubSession(body);
      const { hostId: createdHost, localId } = sessionPath(response.id);

      draftCleared.current = true;

      if (hubUrl !== null) {
        await Promise.all([
          clearCreationDraft(hubUrl),
          clearCreationIntent({ hubUrl, hostId: cleanHostId }),
        ]).catch(() => {
          console.warn('Could not clear the confirmed session draft.');
        });
      }

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
      <View style={styles.container}>
        {hostsLoading || !draftReady ? (
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
            automaticallyAdjustKeyboardInsets
            contentContainerStyle={styles.content}
            keyboardDismissMode="on-drag"
            keyboardShouldPersistTaps="handled"
          >
            {draftError ? (
              <Text style={styles.errorText}>{draftError}</Text>
            ) : null}

            <SelectField
              label="Host"
              onChange={setSelectedHost}
              options={hostOptions}
              placeholder="Select a host"
              value={hostId}
            />

            {selectedHost === undefined ? null : (
              <>
                <SelectField
                  label="Agent"
                  onChange={setSelectedAgent}
                  options={agentOptions}
                  value={agent}
                />
                {isCodex ? (
                  <View style={styles.field}>
                    <TextField
                      label="Find a conversation"
                      value={conversationSearch}
                      onChangeText={setConversationSearch}
                      placeholder="Search prior conversations"
                    />
                    <SelectField
                      label="Conversation"
                      value={nativeThreadId}
                      onChange={setNativeThreadId}
                      options={[
                        { value: '', label: 'New conversation' },
                        ...(nativeThreadId && !selectedConversation
                          ? [
                              {
                                value: nativeThreadId,
                                label: 'Saved conversation',
                                detail: nativeThreadId,
                              },
                            ]
                          : []),
                        ...conversations.map((item) => ({
                          value: item.thread_id,
                          label:
                            item.name ||
                            item.preview ||
                            'Untitled conversation',
                          detail: `${item.cwd} · ${item.status}`,
                        })),
                      ]}
                    />
                    {conversationsLoading ? (
                      <Text style={styles.helperText}>
                        Loading conversations…
                      </Text>
                    ) : conversationsError ? (
                      <Text style={styles.errorText}>{conversationsError}</Text>
                    ) : conversations.length === 0 ? (
                      <Text style={styles.helperText}>
                        No prior conversations found.
                      </Text>
                    ) : null}
                    {conversationCursor ? (
                      <Pressable
                        disabled={conversationsLoading}
                        onPress={() => {
                          void loadMoreConversations();
                        }}
                        style={styles.select}
                      >
                        <Text>Load more conversations</Text>
                      </Pressable>
                    ) : null}
                    {isResume ? (
                      <Text style={styles.helperText}>
                        Codex will use the original working folder.
                      </Text>
                    ) : null}
                  </View>
                ) : null}
                {!isResume ? (
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
                        <Switch
                          onValueChange={setUseRawRepo}
                          value={useRawRepo}
                        />
                      </View>
                    ) : null}
                  </>
                ) : null}

                {agent ? (
                  <View style={styles.field}>
                    <SelectField
                      label="Model"
                      value={model}
                      onChange={(value) => {
                        setModel(value);
                        setReasoning('');
                      }}
                      options={[
                        { value: '', label: 'Host default' },
                        ...(catalog?.status === 'ready'
                          ? catalog.models
                          : []
                        ).map((item) => ({
                          value: item.id,
                          label: item.label,
                          detail: item.description,
                        })),
                      ]}
                    />
                    {catalogLoading ? (
                      <Text style={styles.helperText}>Loading models…</Text>
                    ) : catalogError ? (
                      <Text style={styles.errorText}>{catalogError}</Text>
                    ) : catalog?.status !== 'ready' ? (
                      <Text style={styles.helperText}>
                        {catalog?.message || 'Uses the host default.'}
                      </Text>
                    ) : catalog.models.length === 0 ? (
                      <Text style={styles.helperText}>
                        No model choices are available. Uses the host default.
                      </Text>
                    ) : null}
                    {selectedModel?.reasoning_efforts?.length ? (
                      <SelectField
                        label="Reasoning"
                        value={reasoning}
                        onChange={setReasoning}
                        options={[
                          { value: '', label: 'Host default' },
                          ...selectedModel.reasoning_efforts.map((item) => ({
                            value: item.id,
                            label: item.id,
                            detail: item.description,
                          })),
                        ]}
                      />
                    ) : null}
                  </View>
                ) : null}
                {isCodex ? (
                  <>
                    <CodexSettingsSummary
                      hostId={hostId}
                      onSettings={setHostSettings}
                      cwd={workingCwd}
                    />
                    <SelectField
                      label="Permissions"
                      value={permissionProfile}
                      onChange={setPermissionProfile}
                      options={[
                        { value: '', label: 'Host default' },
                        ...(hostSettings?.permission_profiles.profiles ?? [])
                          .filter((profile) => profile.allowed)
                          .map((profile) => ({
                            value: profile.id,
                            label: profile.id,
                            detail: profile.description,
                          })),
                      ]}
                    />
                  </>
                ) : null}

                {agent === 'claude-cli' ? (
                  <SelectField
                    label="Permission mode"
                    onChange={setPermissionMode}
                    options={permissionModes}
                    value={permissionMode}
                  />
                ) : null}

                {!isResume ? (
                  <>
                    {isCodex ? (
                      <SelectField
                        label="Working folder"
                        value={workspaceMode}
                        onChange={setWorkspaceMode}
                        options={[
                          {
                            value: 'existing',
                            label: 'Use the existing folder',
                            detail: 'Keep its current branch and files.',
                          },
                          {
                            value: 'isolated',
                            label: 'Create an isolated worktree',
                            detail: 'Use a separate branch and working folder.',
                          },
                        ]}
                      />
                    ) : null}
                    <TextField
                      autoCapitalize="none"
                      label="Subfolder"
                      onChangeText={setCwd}
                      placeholder="subdir/within/repo"
                      value={cwd}
                    />

                    {!isCodex || workspaceMode === 'isolated' ? (
                      <>
                        <TextField
                          autoCapitalize="none"
                          label="Branch"
                          onChangeText={setBranch}
                          value={branch}
                        />

                        <View style={styles.field}>
                          <Text style={styles.label}>Base reference</Text>
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
                            <Text style={styles.helperText}>
                              Loading refs...
                            </Text>
                          ) : refsError !== null ? (
                            <Text style={styles.errorText}>{refsError}</Text>
                          ) : null}
                        </View>
                      </>
                    ) : null}
                  </>
                ) : null}

                <TextField
                  label="Message"
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
                      <Text style={styles.submitButtonText}>
                        {isResume ? 'Resume conversation' : 'Start session'}
                      </Text>
                    </>
                  )}
                </Pressable>
              </>
            )}
          </ScrollView>
        )}
      </View>
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
