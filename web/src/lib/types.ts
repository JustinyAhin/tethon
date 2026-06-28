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

export type { RepoInfo, HostSummary, CreateSessionResponse };
