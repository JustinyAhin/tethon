import type { ReactNode } from 'react';
import { Platform, ScrollView, StyleSheet, Text, View } from 'react-native';

import { ApprovalActions } from '@/components/approval-actions';
import { MarkdownOutput } from '@/components/markdown-output';
import {
  EventApprovalResolved,
  EventAwaitingApproval,
  EventDone,
  EventError,
  EventInputReceived,
  EventOutput,
  EventSessionStarted,
  EventToolResult,
  EventUsage,
  type ApprovalAction,
  type ApprovalPayload,
  type ApprovalResolvedPayload,
  type DonePayload,
  type ErrorPayload,
  type InputReceivedPayload,
  type OutputPayload,
  type SessionEvent,
  type SessionStartedPayload,
  type ToolResultPayload,
  type UsagePayload,
} from '@/lib/hub/types';

type SessionEventCardProps = {
  approvalActions?: {
    errors: Record<string, string>;
    submitting: Record<string, boolean>;
    onResolve: (args: {
      action: ApprovalAction;
      requestId: string;
      text?: string;
    }) => void;
  };
  event: SessionEvent;
  resolvedApprovals: Record<string, ApprovalResolvedPayload>;
};

const monospaceFont = Platform.select({ ios: 'Menlo', default: 'monospace' });

export const SessionEventCard = ({
  approvalActions,
  event,
  resolvedApprovals,
}: SessionEventCardProps) => {
  switch (event.type) {
    case EventSessionStarted:
      return renderSessionStarted(event.payload as SessionStartedPayload);
    case EventOutput:
      return renderOutput(event.payload as OutputPayload);
    case EventInputReceived:
      return renderInputReceived(event.payload as InputReceivedPayload);
    case EventAwaitingApproval:
      return renderAwaitingApproval(
        event.payload as ApprovalPayload,
        approvalActions,
        resolvedApprovals,
      );
    case EventApprovalResolved:
      return renderApprovalResolved(event.payload as ApprovalResolvedPayload);
    case EventToolResult:
      return renderToolResult(event.payload as ToolResultPayload);
    case EventUsage:
      return renderUsage(event.payload as UsagePayload);
    case EventDone:
      return renderDone(event.payload as DonePayload);
    case EventError:
      return renderError(event.payload as ErrorPayload);
    default:
      return <JsonPre value={event.payload} />;
  }
};

const renderSessionStarted = (payload: SessionStartedPayload) => (
  <View style={[styles.card, styles.startedCard]}>
    <Text style={styles.cardTitle}>Session started</Text>
    <View style={styles.labelRows}>
      <LabelRow label="repo" value={payload.repo ?? payload.repo_id ?? '-'} />
      <LabelRow label="agent" value={payload.agent ?? '-'} />
      <LabelRow label="branch" value={payload.branch ?? '-'} />
    </View>
    {payload.prompt ? <Text style={styles.prompt}>{payload.prompt}</Text> : null}
  </View>
);

const renderOutput = (payload: OutputPayload) => (
  <View style={[styles.card, styles.outputCard]}>
    <MarkdownOutput source={payload.text} />
  </View>
);

const renderInputReceived = (payload: InputReceivedPayload) => (
  <View style={styles.inputOffset}>
    <View style={[styles.card, styles.inputCard]}>
      <Text style={styles.inputText}>{payload.text}</Text>
    </View>
  </View>
);

const renderAwaitingApproval = (
  payload: ApprovalPayload,
  approvalActions: SessionEventCardProps['approvalActions'],
  resolvedApprovals: Record<string, ApprovalResolvedPayload>,
) => {
  const resolved = resolvedApprovals[payload.request_id];
  const isResolved = resolved !== undefined;

  return (
    <View
      style={[
        styles.card,
        isResolved ? styles.approvalResolvedCard : styles.approvalPendingCard,
      ]}
    >
      <View style={styles.cardHeader}>
        <View style={styles.flexText}>
          <Text
            style={[
              styles.cardTitle,
              isResolved ? styles.resolvedText : styles.pendingText,
            ]}
          >
            {isResolved ? 'Approval resolved' : 'Awaiting approval'}
          </Text>
          <Text
            style={[
              styles.primaryLine,
              isResolved ? styles.resolvedText : styles.pendingText,
            ]}
          >
            {payload.command || payload.tool_name || 'tool request'}
          </Text>
        </View>
        {isResolved ? <Chip label={resolved.action} /> : null}
      </View>

      {payload.reason ? (
        <Text style={isResolved ? styles.dimText : styles.pendingReason}>
          {payload.reason}
        </Text>
      ) : null}
      {payload.agent || payload.tool_name ? (
        <Text style={isResolved ? styles.dimText : styles.pendingMeta}>
          {payload.agent ?? 'agent'}
          {payload.tool_name ? ` · ${payload.tool_name}` : ''}
        </Text>
      ) : null}
      {payload.tool_input !== undefined ? (
        <JsonPre tinted value={payload.tool_input} />
      ) : null}
      {resolved?.text ? (
        <View style={styles.resolvedInfo}>
          <Text style={styles.resolvedReply}>{resolved.text}</Text>
        </View>
      ) : null}
      {!isResolved && approvalActions !== undefined ? (
        <ApprovalActions
          error={approvalActions.errors[payload.request_id] ?? null}
          onResolve={approvalActions.onResolve}
          requestId={payload.request_id}
          submitting={approvalActions.submitting[payload.request_id] ?? false}
        />
      ) : null}
    </View>
  );
};

const renderApprovalResolved = (payload: ApprovalResolvedPayload) => (
  <DimLine>
    approval {payload.action}
    {payload.text ? `: ${payload.text}` : ''}
  </DimLine>
);

const renderToolResult = (payload: ToolResultPayload) => {
  const success = payload.status === 'success';
  const meta = [
    payload.duration_ms != null ? `${payload.duration_ms} ms` : '',
    payload.approval_request_id
      ? `approval ${payload.approval_request_id}`
      : '',
    payload.redacted ? 'redacted' : '',
    payload.truncated ? 'truncated' : '',
  ].filter(Boolean);

  return (
    <View
      style={[
        styles.card,
        success ? styles.toolSuccessCard : styles.toolFailureCard,
      ]}
    >
      <View style={styles.cardHeader}>
        <View style={styles.flexText}>
          <Text
            style={[
              styles.cardTitle,
              success ? styles.toolSuccessText : styles.toolFailureText,
            ]}
          >
            Tool result
          </Text>
          <Text
            style={[
              styles.primaryLine,
              success ? styles.toolSuccessText : styles.toolFailureText,
            ]}
          >
            {payload.tool_name || 'tool'}
            {payload.tool_use_id ? ` · ${payload.tool_use_id}` : ''}
          </Text>
        </View>
        <Chip label={payload.status || 'unknown'} />
      </View>
      {meta.length > 0 ? (
        <Text
          style={[
            styles.toolMeta,
            success ? styles.toolSuccessText : styles.toolFailureText,
          ]}
        >
          {meta.join(' · ')}
        </Text>
      ) : null}
      {payload.tool_input !== undefined ? (
        <JsonPre tinted value={payload.tool_input} />
      ) : null}
      {payload.response !== undefined ? (
        <JsonPre tinted value={payload.response} />
      ) : null}
      {payload.error ? (
        <Text style={[styles.preWrap, styles.toolFailureText]}>
          {payload.error}
        </Text>
      ) : null}
    </View>
  );
};

const renderUsage = (payload: UsagePayload) => (
  <DimLine>
    usage · {payload.tokens?.total_billable ?? 0} tokens
    {payload.cost ? ` · $${payload.cost.total_usd.toFixed(4)}` : ''}
  </DimLine>
);

const renderDone = (payload: DonePayload) => (
  <Text style={styles.doneText}>Session finished ({payload.reason})</Text>
);

const renderError = (payload: ErrorPayload) => (
  <View style={styles.errorBanner}>
    <Text style={styles.errorBannerText}>{payload.message}</Text>
  </View>
);

const LabelRow = ({ label, value }: { label: string; value: string }) => (
  <View style={styles.labelRow}>
    <Text style={styles.label}>{label}</Text>
    <Text style={styles.labelValue}>{value}</Text>
  </View>
);

const JsonPre = ({
  tinted = false,
  value,
}: {
  tinted?: boolean;
  value: unknown;
}) => (
  <ScrollView
    horizontal
    showsHorizontalScrollIndicator
    style={[styles.jsonPre, tinted && styles.jsonPreTinted]}
  >
    <Text style={styles.jsonText}>{formatPayload(value)}</Text>
  </ScrollView>
);

const DimLine = ({ children }: { children: ReactNode }) => (
  <Text style={styles.dimLine}>{children}</Text>
);

const Chip = ({ label }: { label: string }) => (
  <View style={styles.chip}>
    <Text style={styles.chipText}>{label}</Text>
  </View>
);

const formatPayload = (payload: unknown): string => {
  if (payload === null || payload === undefined) {
    return '';
  }

  if (typeof payload === 'string') {
    return payload;
  }

  return JSON.stringify(payload, null, 2);
};

const styles = StyleSheet.create({
  card: {
    borderRadius: 8,
    borderWidth: 1,
    gap: 8,
    padding: 12,
  },
  startedCard: {
    borderColor: '#dce3ea',
    backgroundColor: '#f1f3f5',
  },
  outputCard: {
    borderColor: '#dce3ea',
    backgroundColor: '#ffffff',
  },
  inputOffset: {
    marginLeft: 32,
  },
  inputCard: {
    borderColor: '#c9def0',
    backgroundColor: '#e8f1fa',
  },
  inputText: {
    color: '#2f3a43',
    fontSize: 14,
    lineHeight: 20,
  },
  approvalPendingCard: {
    borderColor: '#ecd9ab',
    backgroundColor: '#fdf1dc',
  },
  approvalResolvedCard: {
    borderColor: '#dce3ea',
    backgroundColor: '#f1f3f5',
  },
  toolSuccessCard: {
    borderColor: '#bfe6cf',
    backgroundColor: '#e7f6ed',
  },
  toolFailureCard: {
    borderColor: '#f3bfbb',
    backgroundColor: '#fbeae9',
  },
  cardHeader: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    gap: 10,
  },
  flexText: {
    flex: 1,
    gap: 3,
  },
  cardTitle: {
    color: '#101820',
    fontSize: 14,
    fontWeight: '700',
    lineHeight: 19,
  },
  primaryLine: {
    color: '#2f3a43',
    fontSize: 13,
    lineHeight: 18,
  },
  labelRows: {
    gap: 5,
  },
  labelRow: {
    gap: 2,
  },
  label: {
    color: '#8a96a3',
    fontSize: 11,
    fontWeight: '700',
    textTransform: 'uppercase',
  },
  labelValue: {
    color: '#2f3a43',
    fontSize: 13,
    lineHeight: 18,
  },
  prompt: {
    color: '#2f3a43',
    fontSize: 13,
    lineHeight: 19,
  },
  pendingText: {
    color: '#9a6700',
  },
  pendingReason: {
    color: '#9a6700',
    fontSize: 12,
    lineHeight: 17,
  },
  pendingMeta: {
    color: '#9a6700',
    fontSize: 12,
    lineHeight: 17,
    opacity: 0.85,
  },
  resolvedText: {
    color: '#5c6873',
  },
  dimText: {
    color: '#8a96a3',
    fontSize: 12,
    lineHeight: 17,
  },
  resolvedInfo: {
    alignItems: 'flex-start',
    gap: 6,
  },
  resolvedReply: {
    color: '#5c6873',
    fontSize: 12,
    lineHeight: 17,
  },
  toolMeta: {
    fontSize: 12,
    lineHeight: 17,
  },
  toolSuccessText: {
    color: '#087443',
  },
  toolFailureText: {
    color: '#b42318',
  },
  preWrap: {
    fontSize: 12,
    lineHeight: 17,
  },
  jsonPre: {
    borderRadius: 6,
    backgroundColor: '#f1f3f5',
    paddingHorizontal: 8,
    paddingVertical: 7,
  },
  jsonPreTinted: {
    backgroundColor: 'rgba(255,255,255,0.7)',
  },
  jsonText: {
    color: '#2f3a43',
    fontFamily: monospaceFont,
    fontSize: 11,
    lineHeight: 16,
  },
  dimLine: {
    color: '#8a96a3',
    fontSize: 12,
    lineHeight: 17,
  },
  doneText: {
    color: '#5c6873',
    fontSize: 13,
    lineHeight: 18,
  },
  chip: {
    borderRadius: 999,
    backgroundColor: '#dce3ea',
    paddingHorizontal: 8,
    paddingVertical: 3,
  },
  chipText: {
    color: '#5c6873',
    fontSize: 11,
    fontWeight: '700',
    lineHeight: 14,
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
});
