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
go checks -> build and push images -> trigger Dokploy redeploy
```

Pull requests run only the Go checks. Deploys only run after green checks on
`main`.

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

## Required GitHub Configuration

Create a Dokploy API key, then set this repository secret:

```txt
DOKPLOY_API_KEY=<Dokploy API key>
```

Set these repository variables:

```txt
DOKPLOY_BASE_URL=https://dokploy.example.com
DOKPLOY_HUB_COMPOSE_ID=QTPBoNrmWM0KsEJ63DmEF
DOKPLOY_HOSTD_COMPOSE_ID=WThL5RaCy4zvffm9asslY
```

The workflow calls `POST /api/compose.redeploy` with the matching `composeId`.
Keep Dokploy auto-deploy disabled for both Compose services; the old
`/api/deploy/compose/...` webhook URLs return `Automatic deployments are
disabled for this compose` when auto-deploy is off.

If the API key, base URL, or a compose ID is missing, that redeploy step is
skipped. Image publishing still happens.

## Self-Update Behavior

Redeploying `tethon-hostd` restarts the VPS host daemon container. Any running
agent sessions on that VPS host die during the restart. Session worktrees are
retained or removed according to the hostd worktree cleanup policy, so failed or
interrupted sessions can still be inspected after the container comes back.
