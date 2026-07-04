# Resource Visibility Policy

Last reviewed: 2026-07-04.

This policy applies to host resource visibility in web and mobile: process
trees, listening ports, runtime age, CPU/memory, worktree/cache footprint, and
orphan warnings.

## Scope

Resource visibility v0 is read-only. It may report what Tethon-owned resources
are doing, but it must not kill processes, delete files, close ports, or mutate
host state. Cleanup belongs in a later issue with a separate permission model
and audit trail.

Resource visibility v0 is scoped to Tethon-owned resources by default:

- the `tethon-hostd` health/status surface,
- active session process trees started by `tethon-hostd`,
- listening ports owned by those process trees,
- per-session worktree/cache paths and sizes managed by Tethon,
- suspected leftover worktrees or processes that can be tied to Tethon-owned
  session records or naming conventions.

Full host-wide process lists, full host-wide port scans, and arbitrary
filesystem inspection are out of scope. Adding any of those requires a future
issue that defines user intent, authorization, UI warnings, and retention rules.

## Excluded Data

Environment variables are never exposed. This includes inherited daemon
environment, process environment, shell-style `KEY=value` assignments observed
in command previews, and values read from config/env files.

The resource snapshot must not expose secret-bearing files or values, including
`.env` files, private keys, bearer tokens, cookies, API keys, passwords, or
authorization headers.

## Command Preview Rule

Command lines are treated as sensitive metadata. The default UI should show an
executable-focused preview, not an unbounded argv dump.

When a command preview is needed:

- bound it to 240 bytes by default,
- redact shell-style environment assignments as `[env]`,
- redact values for secret-bearing flags and key/value arguments,
- redact inline authorization, cookie, token, password, secret, and API-key
  values,
- redact paths using the path rule below,
- mark the snapshot field as redacted/truncated when either happened.

The goal is to help answer "which Tethon-owned process is this?" without
turning the UI into a secret or filesystem browser.

## Path Rule

Paths are sensitive because they can reveal usernames, project names, clients,
and local machine layout. Resource snapshots must bound paths to 160 bytes by
default and should prefer labels over raw absolute paths.

Path display rules:

- paths under a known Tethon-owned root may be shown as a labeled relative path,
  for example `$worktree/src/main.go`;
- paths outside known owned roots are shown as `[external]/basename`;
- secret-bearing paths such as `.env`, `.ssh`, private keys, and known
  credential/config files are shown as `[redacted path]`;
- path fields must carry redacted/truncated metadata when the displayed value
  is not the raw value.

Current host and repo summaries expose registered repo roots/paths to
authenticated clients for session creation. The resource visibility API should
not broaden that exposure: it should use labeled paths for runtime/resource
state unless a later design explicitly changes the privacy model.

## Ports and Process Metadata

Ports may be shown only when the listener belongs to a Tethon-owned process
tree. The snapshot may include protocol, address family, local port, and a
bounded owning-process reference. It should avoid remote peer enumeration in v0.

Process metadata may include pid, parent/child relation, executable basename,
age, coarse CPU/memory, and ownership markers. It must not include environment
variables or unredacted command lines.

## Retention

Resource snapshots are live diagnostics, not session events. Do not append
high-churn resource data to the session event log by default. Persist only
meaningful lifecycle events or future cleanup actions.
