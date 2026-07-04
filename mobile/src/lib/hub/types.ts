export type RepoInfo = {
  id: string;
  name: string;
  path: string;
  default_branch?: string;
};

export type HostSummary = {
  host_id: string;
  name: string;
  url: string;
  agents: string[];
  repo_roots: string[];
  repos?: RepoInfo[];
  last_seen: string;
  online: boolean;
};

export type ListHostsResponse = {
  hosts: HostSummary[];
};
