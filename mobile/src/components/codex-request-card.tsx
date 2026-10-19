import { useState } from 'react';
import {
  Linking,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  ScrollView,
  View,
} from 'react-native';

import type { CodexRequestPayload } from '@/lib/hub/types';
import {
  list,
  record,
  text,
  type TimelineEntry,
} from '@/lib/hub/codex-timeline';

type RequestCardProps = {
  request: CodexRequestPayload;
  disabled: boolean;
  busy: boolean;
  error?: string;
  context?: TimelineEntry;
  fromNotification?: boolean;
  onRespond: (result: unknown) => void;
};

const answerText = (answers: Record<string, string>, id: string): string =>
  Object.hasOwn(answers, id) ? answers[id] : '';

const CodexRequestCard = ({
  request,
  disabled,
  busy,
  error,
  context,
  fromNotification,
  onRespond,
}: RequestCardProps) => {
  const params = record(request.params);

  const blocked =
    disabled ||
    busy ||
    request.unsupported === true ||
    request.truncated === true;
  const command = request.method === 'item/commandExecution/requestApproval';
  const file = request.method === 'item/fileChange/requestApproval';
  const permissions = request.method === 'item/permissions/requestApproval';
  const question = request.method === 'item/tool/requestUserInput';
  const mcp = request.method === 'mcpServer/elicitation/request';
  const network = record(params.networkApprovalContext);
  const targetAvailable =
    (!file || Boolean(context?.text || text(params.grantRoot))) &&
    (!command ||
      Boolean(
        text(params.command) ||
        text(network.host) ||
        list(params.commandActions).length,
      ));
  const supported =
    (command || file || permissions || question || mcp) && targetAvailable;

  const action = (label: string, result: unknown) => (
    <Action
      key={label}
      label={label}
      disabled={blocked}
      onPress={() => onRespond(result)}
    />
  );

  return (
    <View style={styles.card}>
      {fromNotification ? (
        <Text style={styles.detail}>Opened from notification</Text>
      ) : null}

      <Text style={styles.title}>
        {question
          ? 'Your input is needed'
          : mcp
            ? `Tool request · ${text(params.serverName)}`
            : permissions
              ? 'Permission request'
              : file
                ? 'Allow file changes?'
                : command
                  ? params.kind === 'writeStdin'
                    ? 'Allow terminal input?'
                    : 'Allow this command?'
                  : 'Request needs attention'}
      </Text>

      {text(params.command) ? (
        <Text selectable style={styles.context}>
          {text(params.command)}
        </Text>
      ) : null}
      {text(network.host) ? (
        <Text selectable style={styles.context}>
          Network: {text(network.protocol)} · {text(network.host)}
        </Text>
      ) : null}
      {text(params.cwd) ? (
        <Text selectable style={styles.detail}>
          Folder: {text(params.cwd)}
        </Text>
      ) : null}
      {text(params.grantRoot) ? (
        <Text selectable style={styles.detail}>
          Write access: {text(params.grantRoot)}
        </Text>
      ) : null}
      {text(params.reason) ? (
        <Text style={styles.body}>{text(params.reason)}</Text>
      ) : null}
      {file && context?.text ? (
        <ScrollView style={{ maxHeight: 240 }}>
          <Text selectable style={styles.context}>
            {context.text}
          </Text>
        </ScrollView>
      ) : null}
      {command && !text(params.command)
        ? list(params.commandActions).map((value, index) => (
            <Text key={index} selectable style={styles.context}>
              {text(record(value).command)}
            </Text>
          ))
        : null}
      {request.thread_id ? (
        <Text style={styles.detail} numberOfLines={1}>
          Conversation {request.thread_id}
        </Text>
      ) : null}

      {request.unsupported || request.truncated || !supported ? (
        <Text style={styles.body}>
          Complete this request in the host terminal. This app cannot safely
          display its full controls.
        </Text>
      ) : (
        <>
          {command || file ? (
            <View style={styles.actions}>
              {action('Allow once', { decision: 'accept' })}
              {action('Allow for session', { decision: 'acceptForSession' })}
              {action('Decline', { decision: 'decline' })}
              {action('Cancel turn', { decision: 'cancel' })}
              {command && Array.isArray(params.proposedExecpolicyAmendment)
                ? action(
                    `Allow command rule: ${params.proposedExecpolicyAmendment.filter((value) => typeof value === 'string').join(' ')}`,
                    {
                      decision: {
                        acceptWithExecpolicyAmendment: {
                          execpolicy_amendment:
                            params.proposedExecpolicyAmendment,
                        },
                      },
                    },
                  )
                : null}
              {command
                ? list(params.proposedNetworkPolicyAmendments).map(
                    (value, index) => {
                      const amendment = record(value);
                      return text(amendment.host) &&
                        (amendment.action === 'allow' ||
                          amendment.action === 'deny') ? (
                        <Action
                          key={index}
                          label={`${text(amendment.action)} ${text(amendment.host)} for future requests`}
                          disabled={blocked}
                          onPress={() =>
                            onRespond({
                              decision: {
                                applyNetworkPolicyAmendment: {
                                  network_policy_amendment: amendment,
                                },
                              },
                            })
                          }
                        />
                      ) : null;
                    },
                  )
                : null}
            </View>
          ) : null}
          {permissions ? (
            <PermissionControls
              params={params}
              disabled={blocked}
              onRespond={onRespond}
            />
          ) : null}

          {question ? (
            <QuestionControls
              params={params}
              disabled={blocked}
              onRespond={onRespond}
            />
          ) : null}

          {mcp ? (
            <McpControls
              params={params}
              disabled={blocked}
              onRespond={onRespond}
            />
          ) : null}
        </>
      )}

      {disabled ? (
        <Text style={styles.detail}>
          Wait for the host connection before you answer.
        </Text>
      ) : null}
      {busy ? (
        <Text style={styles.detail}>
          Waiting for the host to confirm this answer…
        </Text>
      ) : null}
      {error ? (
        <Text accessibilityRole="alert" style={styles.error}>
          {error}
        </Text>
      ) : null}
    </View>
  );
};

const PermissionControls = ({
  params,
  disabled,
  onRespond,
}: {
  params: Record<string, unknown>;
  disabled: boolean;
  onRespond: (result: unknown) => void;
}) => {
  const permissions = record(params.permissions);
  const filesystem = record(permissions.fileSystem);
  const grants = {
    ...(permissions.network !== null && permissions.network !== undefined
      ? { network: permissions.network }
      : {}),
    ...(permissions.fileSystem !== null && permissions.fileSystem !== undefined
      ? { fileSystem: permissions.fileSystem }
      : {}),
  };

  return (
    <View style={styles.section}>
      {record(permissions.network).enabled === true ? (
        <Text style={styles.body}>Network access requested.</Text>
      ) : null}
      {(['read', 'write'] as const).map((kind) =>
        list(filesystem[kind]).map((path, index) => (
          <Text key={`${kind}:${index}`} selectable style={styles.body}>
            {kind}: {text(path)}
          </Text>
        )),
      )}
      {list(filesystem.entries).map((value, index) => {
        const entry = record(value);

        return (
          <Text key={index} selectable style={styles.body}>
            {text(entry.access)}:{' '}
            {text(record(entry.path).path) ||
              text(record(entry.path).pattern) ||
              text(record(entry.path).value)}
          </Text>
        );
      })}

      <View style={styles.actions}>
        <Action
          label="Allow for this turn"
          disabled={disabled}
          onPress={() => onRespond({ permissions: grants, scope: 'turn' })}
        />
        <Action
          label="Allow for session"
          disabled={disabled}
          onPress={() => onRespond({ permissions: grants, scope: 'session' })}
        />
        <Action
          label="Decline"
          disabled={disabled}
          onPress={() => onRespond({ permissions: {}, scope: 'turn' })}
        />
      </View>
    </View>
  );
};

const QuestionControls = ({
  params,
  disabled,
  onRespond,
}: {
  params: Record<string, unknown>;
  disabled: boolean;
  onRespond: (result: unknown) => void;
}) => {
  const [answers, setAnswers] = useState<Record<string, string>>({});

  const questions = list(params.questions).map(record);
  const valid =
    questions.length > 0 &&
    questions.every(
      (question) =>
        text(question.id) &&
        text(question.question) &&
        answerText(answers, text(question.id)).trim(),
    );

  return (
    <View style={styles.section}>
      {questions.map((question) => {
        const id = text(question.id);

        return (
          <View key={id} style={styles.section}>
            <Text style={styles.title}>{text(question.header)}</Text>
            <Text style={styles.body}>{text(question.question)}</Text>
            {list(question.options).map((option) => {
              const choice = record(option);

              return (
                <Action
                  key={text(choice.label)}
                  label={`${answerText(answers, id) === choice.label ? '✓ ' : ''}${text(choice.label)}${text(choice.description) ? ` · ${text(choice.description)}` : ''}`}
                  disabled={disabled}
                  onPress={() =>
                    setAnswers((current) => ({
                      ...current,
                      [id]: text(choice.label),
                    }))
                  }
                />
              );
            })}
            {question.isOther === true ||
            list(question.options).length === 0 ? (
              <TextInput
                accessibilityLabel={text(question.question)}
                secureTextEntry={question.isSecret === true}
                editable={!disabled}
                style={styles.input}
                value={answerText(answers, id)}
                onChangeText={(value) =>
                  setAnswers((current) => ({ ...current, [id]: value }))
                }
                placeholder="Your answer"
              />
            ) : null}
          </View>
        );
      })}

      <Action
        label="Send answers"
        disabled={disabled || !valid}
        onPress={() =>
          onRespond({
            answers: Object.fromEntries(
              questions.map((question) => [
                text(question.id),
                { answers: [answerText(answers, text(question.id))] },
              ]),
            ),
          })
        }
      />
    </View>
  );
};

const McpControls = ({
  params,
  disabled,
  onRespond,
}: {
  params: Record<string, unknown>;
  disabled: boolean;
  onRespond: (result: unknown) => void;
}) => {
  const [values, setValues] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);

  const schema = record(params.requestedSchema);
  const fields = Object.entries(record(schema.properties));
  const supported =
    ['form', 'openai/form', 'openaiForm'].includes(text(params.mode)) &&
    schema.type === 'object' &&
    fields.every(([, value]) => {
      const field = record(value);

      return (
        ['string', 'number', 'integer', 'boolean'].includes(text(field.type)) &&
        field.oneOf === undefined &&
        field.anyOf === undefined
      );
    });

  const submit = () => {
    const content: Record<string, unknown> = Object.create(null);

    for (const [name, value] of fields) {
      const field = record(value);
      const answer = answerText(values, name);

      if (answer === undefined || answer === '') {
        if (list(schema.required).includes(name)) {
          setFormError(`Enter ${text(field.title) || name}.`);
          return;
        }
        continue;
      }

      if (field.type === 'number' || field.type === 'integer') {
        const number = Number(answer);

        if (
          !Number.isFinite(number) ||
          (field.type === 'integer' && !Number.isInteger(number)) ||
          (typeof field.minimum === 'number' && number < field.minimum) ||
          (typeof field.maximum === 'number' && number > field.maximum)
        ) {
          setFormError(
            `Enter a valid number for ${text(field.title) || name}.`,
          );
          return;
        }

        content[name] = number;
      } else if (field.type === 'boolean') {
        content[name] = answer === 'true';
      } else {
        if (
          (typeof field.minLength === 'number' &&
            answer.length < field.minLength) ||
          (typeof field.maxLength === 'number' &&
            answer.length > field.maxLength)
        ) {
          setFormError(`Check the length of ${text(field.title) || name}.`);
          return;
        }

        content[name] = answer;
      }

      if (Array.isArray(field.enum) && !field.enum.includes(content[name])) {
        setFormError(
          `Select a supported value for ${text(field.title) || name}.`,
        );
        return;
      }

      if (typeof field.pattern === 'string') {
        try {
          if (!new RegExp(field.pattern).test(answer)) {
            setFormError(`Check the format of ${text(field.title) || name}.`);
            return;
          }
        } catch {
          setFormError(
            'Complete this form in the host terminal. Its format rule is unavailable.',
          );
          return;
        }
      }
    }

    setFormError(null);
    onRespond({ action: 'accept', content, _meta: null });
  };

  return (
    <View style={styles.section}>
      <Text style={styles.body}>{text(params.message)}</Text>
      {params.mode === 'url' ? (
        <Action
          label={`Open ${text(params.url)}`}
          disabled={disabled}
          onPress={() => {
            const url = text(params.url);

            if (/^https?:\/\//.test(url)) {
              void Linking.openURL(url).catch(() =>
                setFormError('Could not open this address.'),
              );
            }
          }}
        />
      ) : supported ? (
        fields.map(([name, value]) => {
          const field = record(value);
          const choices =
            field.type === 'boolean'
              ? ['true', 'false']
              : list(field.enum)
                  .filter((value) =>
                    ['string', 'number', 'boolean'].includes(typeof value),
                  )
                  .map(String);

          return (
            <View key={name} style={styles.section}>
              <Text style={styles.title}>
                {text(field.title) || name}
                {list(schema.required).includes(name) ? ' *' : ''}
              </Text>
              {text(field.description) ? (
                <Text style={styles.detail}>{text(field.description)}</Text>
              ) : null}
              {choices.length > 0 ? (
                <View style={styles.actions}>
                  {choices.map((choice) => (
                    <Action
                      key={choice}
                      label={`${answerText(values, name) === choice ? '✓ ' : ''}${choice}`}
                      disabled={disabled}
                      onPress={() =>
                        setValues((current) => ({ ...current, [name]: choice }))
                      }
                    />
                  ))}
                </View>
              ) : (
                <TextInput
                  accessibilityLabel={text(field.title) || name}
                  style={styles.input}
                  editable={!disabled}
                  value={answerText(values, name)}
                  onChangeText={(value) =>
                    setValues((current) => ({ ...current, [name]: value }))
                  }
                  keyboardType={
                    field.type === 'number' || field.type === 'integer'
                      ? 'numbers-and-punctuation'
                      : 'default'
                  }
                />
              )}
            </View>
          );
        })
      ) : (
        <Text style={styles.body}>
          Complete this form in the host terminal. This app cannot display its
          schema.
        </Text>
      )}
      <View style={styles.actions}>
        {supported && params.mode !== 'url' ? (
          <Action label="Send form" disabled={disabled} onPress={submit} />
        ) : null}
        {params.mode === 'url' ? (
          <Action
            label="Completed"
            disabled={disabled}
            onPress={() =>
              onRespond({ action: 'accept', content: null, _meta: null })
            }
          />
        ) : null}
        <Action
          label="Decline"
          disabled={disabled}
          onPress={() =>
            onRespond({ action: 'decline', content: null, _meta: null })
          }
        />
        <Action
          label="Cancel"
          disabled={disabled}
          onPress={() =>
            onRespond({ action: 'cancel', content: null, _meta: null })
          }
        />
      </View>
      {formError ? <Text style={styles.error}>{formError}</Text> : null}
    </View>
  );
};

const Action = ({
  label,
  disabled,
  onPress,
}: {
  label: string;
  disabled: boolean;
  onPress: () => void;
}) => (
  <Pressable
    accessibilityRole="button"
    disabled={disabled}
    onPress={onPress}
    style={({ pressed }) => [
      styles.button,
      disabled && styles.disabled,
      pressed && styles.pressed,
    ]}
  >
    <Text style={styles.buttonText}>{label}</Text>
  </Pressable>
);

const styles = StyleSheet.create({
  card: {
    padding: 14,
    gap: 10,
    borderRadius: 8,
    borderWidth: 1,
    borderColor: '#ecd9ab',
    backgroundColor: '#fffaf0',
  },

  title: { fontSize: 14, fontWeight: '700', color: '#101820' },

  body: { fontSize: 14, lineHeight: 21, color: '#2f3a43' },

  context: { fontSize: 14, lineHeight: 21, color: '#101820' },

  detail: { fontSize: 12, lineHeight: 18, color: '#5c6873' },

  section: { gap: 8 },

  actions: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },

  button: {
    minHeight: 44,
    justifyContent: 'center',
    padding: 10,
    borderRadius: 7,
    borderColor: '#cdd6df',
    borderWidth: 1,
    backgroundColor: '#fff',
  },

  buttonText: { fontSize: 13, color: '#1f4f75' },

  disabled: { opacity: 0.5 },

  pressed: { opacity: 0.7 },

  input: {
    minHeight: 44,
    borderWidth: 1,
    borderColor: '#cdd6df',
    borderRadius: 7,
    padding: 10,
    color: '#101820',
    backgroundColor: '#fff',
  },

  error: { fontSize: 13, color: '#b42318' },
});

export { CodexRequestCard };
