# Build and deployment

This document owns release credentials, automated deployment, and rollback procedures.
Use the [hub](hub-deploy-dokploy.md) or [VPS host](hostd-deploy-dokploy.md) runbook for initial Dokploy setup.

## Control images

The [control workflow](../.github/workflows/control-images.yml) checks Go code and detects changed paths.
Pull requests run checks without publishing images.
On `main`, affected services build and deploy after the checks pass.
Compose-only changes redeploy the matching service without rebuilding its image.
Manual `workflow_dispatch` runs build and deploy both images.

Keep Dokploy's repository source configured.
Disable its push-triggered auto-deploy for both services.
Otherwise, Dokploy can pull the previous image before CI publishes the replacement.

The workflow publishes `main` and immutable 12-character commit tags:

```txt
ghcr.io/justinyahin/tethon-hub:main
ghcr.io/justinyahin/tethon-hub:<short-sha>
ghcr.io/justinyahin/tethon-hostd:main
ghcr.io/justinyahin/tethon-hostd:<short-sha>
```

For private images, configure Dokploy registry credentials with package read access.
Keep package visibility private unless publication is intentional.

Each image deployment updates only its matching image variable through `POST /api/compose.update`.
It then calls `POST /api/compose.redeploy` with the service's `composeId`.
Use this API route with auto-deploy disabled.
The older `/api/deploy/compose/...` webhook uses `refreshToken` and rejects disabled auto-deploy.

Expected result: Dokploy runs the immutable image tag from the successful workflow.
If configuration is missing, deployment fails after any required image publication.
Inspect the failed job before retrying.

## Required GitHub configuration

Set these repository secrets:

```txt
DOKPLOY_API_KEY=<Dokploy API key>
EXPO_TOKEN=<Expo access token for publishing EAS updates>
```

Set these repository variables:

```txt
DOKPLOY_BASE_URL=https://dokploy.example.com
DOKPLOY_HUB_COMPOSE_ID=<hub composeId>
DOKPLOY_HOSTD_COMPOSE_ID=<hostd composeId>
```

Keep `EXPO_TOKEN` only in GitHub Actions secrets.
Do not put it in Dokploy, hostd, or agent session environments.
CI owns mobile update publication.

## Rollback

Set the affected service's image variable to a known-good commit tag:

```txt
TETHON_HUB_IMAGE=ghcr.io/justinyahin/tethon-hub:<short-sha>
TETHON_HOSTD_IMAGE=ghcr.io/justinyahin/tethon-hostd:<short-sha>
```

Redeploy that service in Dokploy.
Check its logs and health under the service runbook.
Removing the override returns the service to the moving `main` image.
A later successful CI image deployment can replace a manual pin.

## Mobile OTA

The [mobile workflow](../.github/workflows/mobile-ota.yml) publishes to the EAS `preview` branch with the `preview` environment.
It runs after pushes to `main` that change the workflow, `mobile/`, or `packages/hub-sdk/`.
Manual `workflow_dispatch` is also available.
The workflow installs dependencies and records the Expo runtime fingerprint.
It then runs:

```txt
bunx eas-cli update --branch preview --auto --environment preview --non-interactive
```

The app belongs to the Expo project `@segbedji/tethon`.
Its [EAS configuration](../mobile/eas.json) defines the internal `preview` profile.
For an authorized native preview build, run from `mobile/` with an authenticated EAS CLI:

```sh
eas build --profile preview --platform ios
```

Install the resulting build on the phone.
OTA updates require a native runtime fingerprint that matches the installed build.
Native modules, plugins, bundle identity, push entitlements, and native configuration changes can require a new build and installation.

To verify an update, close and reopen the installed app.
If the update downloads on that launch, close and reopen it again.
Expected result: the compatible update runs on the next launch.
If it does not apply, inspect the workflow result, preview channel, and runtime fingerprint.

## Web Worker

Set `HUB_BASE_URL` and the accepted `HUB_AUTH_TOKEN` under [web setup](../web/README.md).
Keep the token in a Worker secret.
From `web/`, update that secret when needed:

```sh
bunx wrangler secret put HUB_AUTH_TOKEN
```

For an authorized deployment, build and deploy from `web/`:

```sh
bun run build
bunx wrangler deploy
```

A successful deployment or version promotion updates `web.tethon.dev`.
Check the deployed Hosts page and an active session stream.
A local build does not update the deployed Worker.

## Host restart behavior

Redeploying the VPS host daemon stops its running agent processes.
The cleanup policy retains failed or interrupted work for inspection.
Check active sessions before restarting the host.
Keep the persistent `hostd-data` volume attached.
