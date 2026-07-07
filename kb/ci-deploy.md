# CI Deploy

GitHub Actions publishes the hub and VPS host daemon images on every push to
`main`, then calls Dokploy's Compose API when it is configured.

This replaces Dokploy's Git push auto-deploy for the hub and VPS hostd
services. Leave Dokploy's repository source configured so it can read the
compose files, but disable its push-triggered auto-deploy. Otherwise Dokploy may
redeploy immediately on the Git push and pull the previous `:main` image before
GitHub Actions finishes publishing the new one.

## Workflow

The workflow at `.github/workflows/control-images.yml` runs:

```txt
go checks + change detection -> service image build(s) -> service redeploy(s)
```

Pull requests run Go checks and change detection only. Deploys only run after
green checks on `main`.

Pushes to `main` build and redeploy only the services affected by the changed
paths. Hub-only changes publish and redeploy only the hub image; hostd-only
changes publish and redeploy only the hostd image. Compose-only changes redeploy
the matching service without rebuilding its image. Manual `workflow_dispatch`
runs force both image builds and both redeploys.

Images are pushed to GHCR with both moving `main` tags and immutable short SHA
tags:

```txt
ghcr.io/justinyahin/tethon-hub:main
ghcr.io/justinyahin/tethon-hub:<short-sha>
ghcr.io/justinyahin/tethon-hostd:main
ghcr.io/justinyahin/tethon-hostd:<short-sha>
```

GHCR container packages are private by default when first published. Keep them
private unless there is a deliberate reason to allow anonymous pulls; configure
Dokploy registry credentials with read access to these packages before switching
the services to image-based deploys.

The Dokploy compose files default to the moving `main` tags. To pin or roll
back one service, set the matching Dokploy environment variable to a SHA tag and
redeploy:

```txt
TETHON_HUB_IMAGE=ghcr.io/justinyahin/tethon-hub:<short-sha>
TETHON_HOSTD_IMAGE=ghcr.io/justinyahin/tethon-hostd:<short-sha>
```

Remove the override to return to the latest `main` image.

## Mobile OTA

The workflow at `.github/workflows/mobile-ota.yml` publishes Expo OTA updates
for the dogfood phone build. It runs on pushes to `main` when files under
`mobile/` or `packages/hub-sdk/` change, and it can also be run manually with
`workflow_dispatch`. Server-only pushes do not publish mobile updates.

The workflow installs dependencies with Bun, logs the current Expo fingerprint
runtime hash, then publishes to the EAS `preview` branch with the `preview`
environment:

```txt
bunx eas-cli update --branch preview --auto --environment preview --non-interactive
```

Because the app uses `runtimeVersion.policy = fingerprint`, OTA updates only
apply to preview binaries with a matching native runtime fingerprint. Changes to
native modules, config plugins, native project files, or native app config need
a new EAS build before installed preview apps can receive the JS update.

## Required GitHub Configuration

Create a Dokploy API key, then set this repository secret:

```txt
DOKPLOY_API_KEY=<Dokploy API key>
EXPO_TOKEN=<Expo access token for publishing EAS updates>
```

Keep `EXPO_TOKEN` only in GitHub Actions secrets. Do not add it to Dokploy,
hostd, or any agent session environment; CI is the only mobile publish
authority.

Set these repository variables:

```txt
DOKPLOY_BASE_URL=https://dokploy.example.com
DOKPLOY_HUB_COMPOSE_ID=<hub composeId>
DOKPLOY_HOSTD_COMPOSE_ID=<hostd composeId>
```

The workflow calls `POST /api/compose.redeploy` with the matching `composeId`.
Keep Dokploy auto-deploy disabled for both Compose services; the old
`/api/deploy/compose/...` webhook URLs use each service's `refreshToken`, not
the internal `composeId`, and return `Automatic deployments are disabled for
this compose` when auto-deploy is off.

If the API key, base URL, or a compose ID is missing, the matching redeploy step
fails with a specific missing-config error after any required image publishing
has completed.

## Self-Update Behavior

Redeploying `tethon-hostd` restarts the VPS host daemon container. Any running
agent sessions on that VPS host die during the restart. Session worktrees are
retained or removed according to the hostd worktree cleanup policy, so failed or
interrupted sessions can still be inspected after the container comes back.
