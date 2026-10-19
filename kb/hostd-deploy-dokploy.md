# VPS host deployment with Dokploy

Deploy the host daemon as a separate Compose service beside the hub.
A hub redeployment must not restart host agent sessions.
Use [CI operations](ci-deploy.md) for publishing, redeployment, rollback, and restart effects.
Track remaining VPS work in Beads under `tethon-ttb`.

## Dokploy app

Configure these settings:

| Setting      | Value                             |
| ------------ | --------------------------------- |
| Source       | This GitHub repository            |
| Branch       | `main`                            |
| Compose path | `deploy/hostd/docker-compose.yml` |
| Domains      | None                              |

Configure registry read access if the image is private.
Set these secrets in Dokploy:

- `TETHON_HUB_AUTH_TOKEN`: a token accepted by the hub.
- `CLAUDE_CODE_OAUTH_TOKEN`: the result of `claude setup-token` on the Mac.

Do not set `ANTHROPIC_API_KEY` for this subscription-backed setup.
The adapter's refusal guard remains enabled.
The [Compose file](../deploy/hostd/docker-compose.yml) owns runtime settings and the `hostd-data` volume.

## First deploy

With an empty repository volume, strict refresh prevents startup.
The first deployment creates the volume needed for repository setup.
The daemon does not clone repositories.

Find the image and volume on the VPS:

```bash
docker images | grep hostd
docker volume ls | grep hostd-data
```

Open the volume with the host image:

```bash
docker run --rm -it --user tethon -v <VOLUME>:/var/lib/tethon --entrypoint bash <IMAGE>
git config --global user.name "Justin Ahinon"
git config --global user.email "tethon@example.com"
git config --global credential.helper store
git clone https://github.com/<owner>/tethon.git /var/lib/tethon/repos/tethon
exit
```

For a private repository, use a fine-grained GitHub token from [token settings](https://github.com/settings/personal-access-tokens/new).
Limit access to the required repository with Contents read/write permission.
At the clone prompt, enter the GitHub username and use the token as the password.
`credential.helper store` saves that credential unencrypted in the volume-backed home directory.
Local Mac or VPS SSH configuration does not apply inside this container.
SSH use requires a client and separately provisioned key material.

Restart the service after cloning the repository.
Expected result: strict refresh succeeds and the daemon registers with the hub.

## Repository refresh

The [entrypoint](../deploy/hostd/entrypoint.sh) refreshes registered roots before daemon startup:

```bash
git fetch --prune --tags origin
git switch <origin default branch>
git merge --ff-only origin/<origin default branch>
```

Strict mode rejects local changes, fetch failures, and non-fast-forward updates.
`-refresh-repos-on-session` also refreshes the selected repository before creating its session worktree.
Keep agent changes in worktrees, not the base repository under `/var/lib/tethon/repos`.

If repository refresh is broken, repair it before restarting normally.
Set `TETHON_HOSTD_REFRESH_REPOS=0` only for emergency startup with known stale repositories.
Restore strict mode after repair.

## Verify

Check hub logs for `hub_host_registered` with `vps-1`.
Check host logs for `hub_reverse_connected`.
Inside the host container, run:

```bash
curl -fsS http://127.0.0.1:39181/status
```

Expected result: host ID `vps-1`, repository `tethon`, and agent `claude-cli`.
For a real session check, use the [first-session workflow](first-remote-agent-session.md#4-start-a-session) with the VPS host.
If registration fails, inspect the accepted token and outbound hub connection.
If startup fails, inspect repository refresh logs and volume contents.
