// Hub API payload shapes (mirror control/internal/protocol).

type RepoInfo = {
	id: string;
	name: string;
	path: string;
	default_branch?: string;
};

type HostSummary = {
	host_id: string;
	name: string;
	url: string;
	agents: string[];
	repo_roots: string[];
	repos?: RepoInfo[];
	last_seen: string; // RFC3339
	online: boolean;
};

type CreateSessionResponse = {
	id: string; // "host_id/local_id"
	status: string; // e.g. "running"
};

type SessionSummary = {
	id: string; // "host_id/local_id"
	host_id: string;
	repo_id?: string;
	repo: string;
	cwd: string;
	branch: string;
	base_ref?: string;
	agent: string;
	backend?: string;
	cli?: string;
	model?: string;
	status: string;
	created_at: string; // RFC3339
	updated_at: string;
	last_seq: number;
};

type SessionEventType =
	| 'session_started'
	| 'output'
	| 'input_received'
	| 'awaiting_approval'
	| 'approval_resolved'
	| 'usage'
	| 'done'
	| 'error';

type SessionEvent = {
	session_id: string;
	seq: number;
	type: SessionEventType;
	created_at: string;
	payload: unknown;
};

type SessionStartedPayload = {
	repo?: string;
	cwd?: string;
	branch?: string;
	base_ref?: string;
	agent?: string;
	prompt?: string;
};

type OutputPayload = { text: string };

type InputReceivedPayload = { text: string };

type ApprovalPayload = {
	request_id: string;
	command: string;
	reason: string;
	tool_name?: string;
	tool_input?: unknown;
	agent?: string;
};

type ApprovalResolvedPayload = {
	request_id: string;
	action: string;
	text?: string;
};

type UsagePayload = {
	agent: string;
	backend: string;
	cli: string;
	model?: string;
	tokens: { total_billable?: number; input?: number; output?: number };
	cost?: { total_usd: number; currency: string };
};

type DonePayload = { reason: string };

type ErrorPayload = { message: string };

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
