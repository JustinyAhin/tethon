import type {
  CodexEventPayload,
  CodexHistoryTurn,
  SessionEvent,
} from '@/lib/hub/types';

type NativeRecord = Record<string, unknown>;
type NativeTurn = CodexHistoryTurn;

type TimelineEntry = {
  id: string;
  title: string;
  text: string;
  detail?: string;
  markdown?: boolean;
  warning?: boolean;
};

const record = (value: unknown): NativeRecord =>
  typeof value === 'object' && value !== null && !Array.isArray(value)
    ? (value as NativeRecord)
    : {};

const text = (value: unknown): string =>
  typeof value === 'string' ? value : '';

const list = (value: unknown): unknown[] =>
  Array.isArray(value) ? value : [];

const contentText = (value: unknown): string => {
  if (typeof value === 'string') {
    return value;
  }

  return list(value)
    .map((part) => {
      const item = record(part);

      return (
        text(item.text) ||
        (item.type === 'image' || item.type === 'localImage'
          ? 'Image attachment'
          : '')
      );
    })
    .filter(Boolean)
    .join('\n');
};

const itemEntry = ({
  item,
  turnId,
}: {
  item: NativeRecord;
  turnId: string;
}): TimelineEntry => {
  const id = `${turnId}:${text(item.id)}`;
  const detail = text(item.status);

  switch (item.type) {
    case 'userMessage':
      return { id, title: 'You', text: contentText(item.content) };

    case 'agentMessage':
      return { id, title: 'Codex', text: text(item.text), markdown: true };

    case 'plan':
      return { id, title: 'Plan', text: text(item.text), markdown: true };

    case 'reasoning':
      return {
        id,
        title: 'Reasoning summary',
        text: list(item.summary)
          .filter((value) => typeof value === 'string')
          .join('\n'),
      };

    case 'commandExecution':
      return {
        id,
        title: text(item.command) || 'Command',
        text: text(item.aggregatedOutput),
        detail: [
          text(item.cwd),
          detail,
          typeof item.exitCode === 'number' ? `Exit ${item.exitCode}` : '',
        ]
          .filter(Boolean)
          .join(' · '),
      };

    case 'fileChange':
      return {
        id,
        title: 'File changes',
        text: list(item.changes)
          .map((value) => {
            const change = record(value);
            return `${text(change.path)}\n${text(change.diff)}`;
          })
          .join('\n\n'),
        detail,
      };

    case 'mcpToolCall':
      return {
        id,
        title:
          [text(item.server), text(item.tool)]
            .filter(Boolean)
            .join(' · ') || 'Tool',
        text:
          contentText(record(item.result).content) ||
          text(record(item.error).message),
        detail,
      };

    case 'dynamicToolCall':
      return {
        id,
        title: text(item.tool) || 'Tool',
        text: contentText(item.contentItems),
        detail,
      };

    case 'functionCallOutput':
      return {
        id,
        title: text(item.name) || 'Tool result',
        text: contentText(item.output),
      };

    case 'collabAgentToolCall':
      return {
        id,
        title: `Agent · ${text(item.tool)}`,
        text: [
          text(item.prompt),
          ...Object.entries(record(item.agentsStates)).map(
            ([agent, state]) =>
              `${agent}: ${text(record(state).status)} ${text(record(state).message)}`,
          ),
        ]
          .filter(Boolean)
          .join('\n'),
        detail,
      };

    case 'subAgentActivity':
      return {
        id,
        title: 'Agent activity',
        text: [text(item.agentPath), text(item.kind), text(item.model)]
          .filter(Boolean)
          .join(' · '),
      };

    case 'webSearch':
      return {
        id,
        title: 'Web search',
        text: text(item.query) || text(record(item.action).query),
        detail,
      };

    case 'contextCompaction':
      return { id, title: 'Context compacted', text: '' };
    default:
      return {
        id,
        title: 'Activity',
        text: text(item.text) || text(item.type),
        detail: 'This activity needs a newer viewer.',
      };
  }
};

const buildCodexTimeline = ({
  events,
  turns,
  currentTurnId,
}: {
  events: SessionEvent[];
  turns: NativeTurn[];
  currentTurnId?: string;
}): TimelineEntry[] => {
  const entries = new Map<string, TimelineEntry>();
  const entryTurns = new Map<string, string>();
  const turnOrder = new Map(turns.map((turn, index) => [turn.id, index]));
  const historyTurns = new Set(turnOrder.keys());
  const eventTurnStarts = new Map<string, number>();

  for (const event of events) {
    if (event.type !== 'codex_event') {
      continue;
    }

    const payload = event.payload as CodexEventPayload;
    const params = record(payload.params);
    const turnId =
      payload.turn_id ||
      text(params.turnId) ||
      text(record(params.turn).id);

    if (turnId && !eventTurnStarts.has(turnId)) {
      eventTurnStarts.set(turnId, event.seq);
    }
  }

  const latestHistoryId = turns.at(-1)?.id;
  const historyAnchor =
    latestHistoryId === undefined
      ? undefined
      : eventTurnStarts.get(latestHistoryId);
  const historyIDs = new Set<string>();
  const liveIDs = new Set(
    events
      .filter((event) => event.type === 'codex_event')
      .map((event) => {
        const payload = event.payload as CodexEventPayload;
        const params = record(payload.params);
        return `${payload.turn_id || text(params.turnId)}:${payload.item_id || text(params.itemId) || text(record(params.item).id)}`;
      }),
  );

  const setTurnEntry = (entry: TimelineEntry, turnId: string) => {
    entries.set(entry.id, entry);
    entryTurns.set(entry.id, turnId);
  };

  for (const turn of turns) {
    for (const value of turn.items) {
      const item = record(value);

      if (!text(item.id)) {
        continue;
      }

      const entry = itemEntry({
        item:
          turn.status === 'interrupted' && item.status === 'inProgress'
            ? { ...item, status: 'interrupted' }
            : item,
        turnId: turn.id,
      });

      if (turn.status === 'inProgress' && liveIDs.has(entry.id)) {
        continue;
      }

      setTurnEntry(entry, turn.id);

      if (turn.status !== 'inProgress') {
        historyIDs.add(entry.id);
      }
    }
  }

  for (const event of events) {
    if (event.type !== 'codex_event') {
      continue;
    }

    const payload = event.payload as CodexEventPayload;
    const params = record(payload.params);
    const turnId = payload.turn_id || text(params.turnId);
    const item = record(params.item);
    const itemId = payload.item_id || text(params.itemId) || text(item.id);
    const id = `${turnId}:${itemId}`;

    // Older delivery fragments cannot replace a native history page.
    if (
      turns.length > 0 &&
      turnId &&
      !historyTurns.has(turnId) &&
      turnId !== currentTurnId &&
      (payload.method.startsWith('item/') ||
        payload.method.startsWith('turn/')) &&
      (historyAnchor === undefined ||
        (eventTurnStarts.get(turnId) ?? event.seq) < historyAnchor)
    ) {
      continue;
    }

    if (turnId && !turnOrder.has(turnId)) {
      turnOrder.set(turnId, turnOrder.size);
    }

    if (
      payload.method === 'hook/started' ||
      payload.method === 'hook/completed'
    ) {
      const run = record(params.run);
      const hookId = `hook:${text(run.id)}`;

      if (run.status === 'completed' || run.status === 'stopped') {
        entries.delete(hookId);
      } else {
        entries.set(hookId, {
          id: hookId,
          title:
            run.status === 'running'
              ? 'Host hook running'
              : 'Host hook needs attention',
          text:
            text(run.statusMessage) ||
            list(run.entries)
              .map((entry) => text(record(entry).text))
              .filter(Boolean)
              .join('\n'),
          detail: [text(run.eventName), text(run.status)]
            .filter(Boolean)
            .join(' · '),
          warning: run.status === 'failed' || run.status === 'blocked',
        });
      }

      continue;
    }

    if (payload.method === 'mcpServer/startupStatus/updated') {
      const serverId = `mcp-startup:${text(params.name)}`;

      if (params.status === 'ready' || params.status === 'cancelled') {
        entries.delete(serverId);
      } else {
        entries.set(serverId, {
          id: serverId,
          title: `MCP · ${text(params.name)}`,
          text:
            text(params.error) ||
            (params.status === 'failed'
              ? 'Server startup failed. Check the host settings.'
              : 'Server is starting.'),
          detail: text(params.failureReason),
          warning: params.status === 'failed',
        });
      }

      continue;
    }

    if (payload.truncated) {
      entries.set(`notice:${event.seq}`, {
        id: `notice:${event.seq}`,
        title: 'Output incomplete',
        text: 'This activity exceeded the delivery limit. Open the host terminal for the full output.',
        warning: true,
      });
      continue;
    }

    if (
      payload.method === 'item/started' ||
      payload.method === 'item/completed'
    ) {
      if (!itemId || historyIDs.has(id)) {
        continue;
      }

      setTurnEntry(
        itemEntry({ item: { ...item, id: itemId }, turnId }),
        turnId,
      );
      continue;
    }

    const delta = text(params.delta);

    if (
      [
        'item/agentMessage/delta',
        'item/plan/delta',
        'item/commandExecution/outputDelta',
        'item/fileChange/outputDelta',
        'item/reasoning/summaryTextDelta',
      ].includes(payload.method) &&
      itemId
    ) {
      if (historyIDs.has(id)) {
        continue;
      }

      const previous = entries.get(id);
      const title = payload.method.includes('agentMessage')
        ? 'Codex'
        : payload.method.includes('plan')
          ? 'Plan'
          : payload.method.includes('command')
            ? 'Command output'
            : payload.method.includes('reasoning')
              ? 'Reasoning summary'
              : 'Tool output';
      setTurnEntry(
        {
          id,
          title: previous?.title ?? title,
          text: `${previous?.text ?? ''}${delta}`,
          detail: previous?.detail,
          markdown: title === 'Codex' || title === 'Plan',
        },
        turnId,
      );
      continue;
    }

    if (payload.method === 'turn/plan/updated') {
      const planId = `${turnId}:plan`;
      setTurnEntry(
        {
          id: planId,
          title: 'Plan',
          text: [
            text(params.explanation),
            ...list(params.plan).map((value) => {
              const step = record(value);
              return `${text(step.status)} · ${text(step.step)}`;
            }),
          ]
            .filter(Boolean)
            .join('\n'),
        },
        turnId,
      );
    } else if (payload.method === 'turn/diff/updated') {
      const diffId = `${turnId}:diff`;
      setTurnEntry(
        {
          id: diffId,
          title: 'Changes',
          text: text(params.diff),
        },
        turnId,
      );
    } else if (payload.method === 'thread/tokenUsage/updated') {
      const usage = record(record(params.tokenUsage).total);
      entries.set('usage', {
        id: 'usage',
        title: 'Usage',
        text:
          typeof usage.totalTokens === 'number'
            ? `${usage.totalTokens.toLocaleString()} tokens`
            : 'Usage updated',
      });
    } else if (payload.method === 'item/mcpToolCall/progress' && itemId) {
      if (historyIDs.has(id)) {
        continue;
      }

      const previous = entries.get(id);
      setTurnEntry(
        {
          id,
          title: previous?.title ?? 'Tool progress',
          text: [previous?.text, text(params.message)]
            .filter(Boolean)
            .join('\n'),
          detail: previous?.detail,
        },
        turnId,
      );
    } else if (
      payload.method === 'item/fileChange/patchUpdated' &&
      itemId
    ) {
      if (historyIDs.has(id)) {
        continue;
      }

      setTurnEntry(
        itemEntry({
          item: {
            type: 'fileChange',
            id: itemId,
            changes: params.changes,
            status: 'in progress',
          },
          turnId,
        }),
        turnId,
      );
    } else if (payload.method === 'tethon/inputError') {
      const noticeId = `input-error:${event.seq}`;

      entries.set(noticeId, {
        id: noticeId,
        title: 'Message needs review',
        text:
          text(params.message) ||
          'The host could not confirm the first message.',
        detail:
          'The conversation was kept. Check native history or the host terminal before sending another message.',
        warning: true,
      });
    } else if (
      payload.method === 'error' ||
      payload.method === 'warning' ||
      payload.method === 'configWarning'
    ) {
      const message =
        text(record(params.error).message) ||
        text(params.message) ||
        text(params.summary) ||
        'Open the host terminal to inspect this error.';
      const noticeId =
        payload.method === 'error'
          ? `error:${event.seq}`
          : `warning:${message}`;

      entries.set(noticeId, {
        id: noticeId,
        title: 'Codex needs attention',
        text: message,
        warning: true,
      });
    } else if (
      ![
        'thread/started',
        'thread/readState/changed',
        'thread/goal/cleared',
        'remoteControl/status/changed',
        'turn/moderationMetadata',
        'thread/status/changed',
        'thread/name/updated',
        'thread/queue/changed',
        'thread/settings/updated',
        'thread/archived',
        'thread/unarchived',
        'thread/closed',
        'thread/compacted',
        'turn/started',
        'turn/completed',
        'serverRequest/resolved',
        'account/updated',
        'account/rateLimits/updated',
        'item/reasoning/textDelta',
        'item/reasoning/summaryPartAdded',
      ].includes(payload.method)
    ) {
      entries.set(`unsupported:${payload.method}`, {
        id: `unsupported:${payload.method}`,
        title: 'Activity needs attention',
        text: `This viewer cannot display ${payload.method}. Open the host terminal.`,
      });
    }
  }

  return [...entries.values()].sort((left, right) => {
    const leftTurn = entryTurns.get(left.id);
    const rightTurn = entryTurns.get(right.id);

    return (
      (leftTurn === undefined
        ? Infinity
        : (turnOrder.get(leftTurn) ?? Infinity)) -
      (rightTurn === undefined
        ? Infinity
        : (turnOrder.get(rightTurn) ?? Infinity))
    );
  });
};

export {
  buildCodexTimeline,
  record,
  text,
  list,
  type NativeTurn,
  type TimelineEntry,
};
