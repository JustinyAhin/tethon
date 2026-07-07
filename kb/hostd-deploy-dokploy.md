# Hostd Dokploy deploy stub

This is the minimal VPS hostd bring-up note for `deploy/hostd/`. The full
runbook polish belongs with ttb.5.

The compose file defaults to the `ghcr.io/justinyahin/tethon-hostd:main` image
published by CI. See `kb/ci-deploy.md` for Dokploy API setup, rollback, and
self-update behavior.

On container startup, the hostd image refreshes git repositories under
`/var/lib/tethon/repos` before starting the daemon. The default Dokploy compose
sets `TETHON_HOSTD_REFRESH_REPOS=strict`, so startup fails if a repo cannot
fetch or fast-forward cleanly instead of serving stale code. Set
`TETHON_HOSTD_REFRESH_REPOS=0` only for emergency startup when repo sync is
known to be broken and stale repos are acceptable.

The VPS compose also starts hostd with `-refresh-repos-on-session`, so a mobile,
web, or CLI session refreshes the selected repo again immediately before its
per-session worktree is created.

GitHub container packages are private by default. Configure Dokploy registry
credentials with read access to `ghcr.io/justinyahin/tethon-hostd` unless you
intentionally make the package public.

## Dokploy app

Create a new Compose service in the existing Tethon project. Keep it separate
from the hub app so hub redeploys do not stop running hostd sessions.

- Source: GitHub repo
- Branch: `main`
- Compose Path: `deploy/hostd/docker-compose.yml`
- Domains: none

Set these env vars in Dokploy:

- `TETHON_HUB_AUTH_TOKEN`: same token accepted by the hub
- `CLAUDE_CODE_OAUTH_TOKEN`: run `claude setup-token` on the Mac and copy the
  printed token

Do not set `ANTHROPIC_API_KEY` for this stack. The hostd Claude adapter keeps
its refusal guard enabled for that env var.

## First deploy

The first deploy is expected to crash-loop because `/var/lib/tethon/repos`
contains no git repositories. That is useful: the image has built, and Dokploy
has created the `hostd-data` volume. Hostd does not clone repos itself.

Find the image and volume on the VPS:

```bash
docker images | grep hostd
docker volume ls | grep hostd-data
```

Initialize the volume:

```bash
docker run --rm -it --user tethon -v <VOLUME>:/var/lib/tethon --entrypoint bash <IMAGE>
git config --global user.name "Justin Ahinon"
git config --global user.email "tethon@example.com"
git config --global credential.helper store
git clone https://github.com/<owner>/tethon.git /var/lib/tethon/repos/tethon
exit
```

For a private repo, use a GitHub username and fine-grained PAT when clone
prompts. GitHub account passwords do not work for HTTPS git operations.

Create the token at:

```text
https://github.com/settings/personal-access-tokens/new
```

Use a fine-grained token with:

- Resource owner: the repo owner
- Repository access: only the `tethon` repository
- Repository permissions: Contents read/write

When `git clone` prompts, enter the GitHub username as the username and paste
the token as the password. `credential.helper store` writes it under the
volume-backed home directory so later fetch/push operations can reuse it.

Local SSH setup on the Mac or VPS host does not apply inside this container.
Using SSH later would require adding an SSH client and provisioning a deploy key
or mounted key material into the hostd volume.

After the repo is initialized, subsequent hostd container starts run:

```bash
git fetch --prune --tags origin
git switch <origin default branch>
git merge --ff-only origin/<origin default branch>
```

Repos with local changes or non-fast-forward updates are rejected in strict mode.
Agent sessions should make changes in per-session worktrees, not in the base
repo under `/var/lib/tethon/repos`.

## Verify

After the next restart, check hub logs for `hub_host_registered` with `vps-1`
and reverse WebSocket connection logs. On the hostd container:

```bash
curl -fsS http://127.0.0.1:39181/status
```

The response should show host id `vps-1`, repo `tethon`, and `claude-cli` in
the agent list. Stop there; starting a real dogfood session is ttb.3.
