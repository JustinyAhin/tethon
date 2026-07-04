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
} from '@tethon/hub-sdk/protocol';
import type { Event as ProtocolEvent } from '@tethon/hub-sdk/protocol';

export type ApprovalAction = 'approve' | 'deny' | 'reply';

export type SessionEventType =
  | typeof EventSessionStarted
  | typeof EventOutput
  | typeof EventInputReceived
  | typeof EventAwaitingApproval
  | typeof EventApprovalResolved
  | typeof EventToolResult
  | typeof EventUsage
  | typeof EventDone
  | typeof EventError;

export type SessionEvent = Omit<ProtocolEvent, 'type'> & {
  type: SessionEventType;
};

export {
  EventApprovalResolved,
  EventAwaitingApproval,
  EventDone,
  EventError,
  EventInputReceived,
  EventOutput,
  EventSessionStarted,
  EventToolResult,
  EventUsage,
};

export type {
  ApprovalPayload,
  ApprovalResolvedPayload,
  DonePayload,
  ErrorPayload,
  InputRequest,
  InputReceivedPayload,
  RepoInfo,
  HostSummary,
  ListHostsResponse,
  OutputPayload,
  ResolveApprovalRequest,
  SessionSummary,
  SessionStartedPayload,
  ListSessionsResponse,
  ToolResultPayload,
  UsagePayload,
} from '@tethon/hub-sdk/protocol';
