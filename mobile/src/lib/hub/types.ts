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
  EventCodexState,
  EventCodexEvent,
  EventCodexRequest,
  EventCodexRequestResolved,
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
  | typeof EventError
  | typeof EventCodexState
  | typeof EventCodexEvent
  | typeof EventCodexRequest
  | typeof EventCodexRequestResolved;

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
  EventCodexState,
  EventCodexEvent,
  EventCodexRequest,
  EventCodexRequestResolved,
};

export type {
  ApprovalPayload,
  ApprovalResolvedPayload,
  CreateSessionRequest,
  CreateSessionResponse,
  DonePayload,
  ErrorPayload,
  InputRequest,
  InputReceivedPayload,
  RepoInfo,
  RepoRefsResponse,
  RegisterDeviceRequest,
  RegisterDeviceResponse,
  HostSummary,
  ListHostsResponse,
  OutputPayload,
  ResolveApprovalRequest,
  SessionSummary,
  SessionStartedPayload,
  ListSessionsResponse,
  ToolResultPayload,
  UsagePayload,
  CodexSessionState,
  CodexEventPayload,
  CodexRequestPayload,
  CodexRequestResolvedPayload,
  CodexRespondRequest,
  ModelCatalog,
  ModelInfo,
  CodexSessionResponse,
  CodexHistoryResponse,
  CodexHistoryTurn,
  CodexConversation,
  CodexConversationsResponse,
  CodexSettingsResponse,
} from '@tethon/hub-sdk/protocol';
