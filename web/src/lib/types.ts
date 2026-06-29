import {
	EventApprovalResolved,
	EventAwaitingApproval,
	EventDone,
	EventError,
	EventInputReceived,
	EventOutput,
	EventSessionStarted,
	EventUsage
} from '$lib/generated/protocol';
import type {
	ApprovalPayload,
	ApprovalResolvedPayload,
	CreateSessionResponse,
	DonePayload,
	ErrorPayload,
	Event as ProtocolEvent,
	HostSummary,
	InputReceivedPayload,
	OutputPayload,
	RepoInfo,
	SessionStartedPayload,
	SessionSummary as ProtocolSessionSummary,
	UsagePayload
} from '$lib/generated/protocol';

type SessionEventType =
	| typeof EventSessionStarted
	| typeof EventOutput
	| typeof EventInputReceived
	| typeof EventAwaitingApproval
	| typeof EventApprovalResolved
	| typeof EventUsage
	| typeof EventDone
	| typeof EventError;

type SessionEvent = Omit<ProtocolEvent, 'type'> & {
	type: SessionEventType;
};

type SessionSummary = ProtocolSessionSummary & {
	host_id: string;
};

export {
	EventApprovalResolved,
	EventAwaitingApproval,
	EventDone,
	EventError,
	EventInputReceived,
	EventOutput,
	EventSessionStarted,
	EventUsage
};
export type {
	RepoInfo,
	HostSummary,
	CreateSessionResponse,
	SessionSummary,
	SessionEventType,
	SessionEvent,
	SessionStartedPayload,
	OutputPayload,
	InputReceivedPayload,
	ApprovalPayload,
	ApprovalResolvedPayload,
	UsagePayload,
	DonePayload,
	ErrorPayload
};
