# Hub deployment with Dokploy

Deploy `tethon-hub` as a Docker Compose service on the VPS.
Dokploy provides the public domain, HTTPS, proxy, and logs.
Use [CI operations](ci-deploy.md) for image publishing, redeployment, and rollback.

## Prerequisites

- Dokploy on the VPS.
- DNS access for `hub.tethon.dev`.
- Registry access if the GHCR image is private.

Keep the hub outside Cloudflare Access until clients support its service-token headers.
Cloudflare DNS proxying can remain enabled.

## Dokploy setup

1. Point the domain's `A` record at the VPS.
2. Add an `AAAA` record if the VPS has IPv6.
3. Create a Compose service with these settings:

   | Setting        | Value                           |
   | -------------- | ------------------------------- |
   | Source         | This GitHub repository          |
   | Branch         | `main`                          |
   | Compose type   | Docker Compose                  |
   | Compose path   | `deploy/hub/docker-compose.yml` |
   | Domain         | `hub.tethon.dev`                |
   | Service        | `hub`                           |
   | Container port | `39180`                         |
   | HTTPS          | Enabled                         |

4. Configure registry credentials if needed.
5. Set the required secret:

   ```txt
   TETHON_HUB_AUTH_TOKEN=<strong-random-token>
   ```

6. Keep the `hub-data` volume attached.
7. Deploy the service.

The [Compose file](../deploy/hub/docker-compose.yml) owns image, environment, and volume settings.
The hub stores push-device registrations in `/var/lib/tethon/hub.db`.
The host registry remains in memory.

Optional settings:

- `TETHON_HUB_AUTH_TOKENS` accepts additional tokens during rotation.
- `TETHON_EXPO_ACCESS_TOKEN` is needed only when Expo Push Security is enabled.

Use Dokploy's domain routing before adding manual Traefik labels.
Disable push-triggered Dokploy auto-deploy under the [CI policy](ci-deploy.md#control-images).
Configure the web app through [web setup](../web/README.md) and [Worker deployment](ci-deploy.md#web-worker).
Use an accepted hub token for the web Worker, hosts, CLI, and mobile app.

## Verify

Check the public endpoint:

```sh
curl -fsS https://hub.tethon.dev/healthz
```

Expected result: HTTP `200`.
This endpoint checks the hub process and proxy, not host connectivity.

Check authentication:

```sh
curl -i https://hub.tethon.dev/hosts
```

Expected result: `401 Unauthorized`.
With an accepted token, list hosts:

```sh
export TETHON_HUB_AUTH_TOKEN='<same-token>'
curl -fsS -H "Authorization: Bearer $TETHON_HUB_AUTH_TOKEN" https://hub.tethon.dev/hosts
```

Expected result: HTTP `200` and a host list.
The list can be empty before a host registers.

After a phone registers for push, send a test notification:

```sh
curl -fsS \
  -H "Authorization: Bearer $TETHON_HUB_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST https://hub.tethon.dev/push/test \
  -d '{"title":"Tethon","body":"Push test"}'
```

Test the approval notification shape:

```sh
curl -fsS \
  -H "Authorization: Bearer $TETHON_HUB_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -X POST https://hub.tethon.dev/push/approvals/test \
  -d '{"host_id":"mac","session_id":"ses_test","request_id":"approval_test","agent":"claude-cli","tool_name":"Bash","repo":"tethon"}'
```

The phone should receive each notification.
The sample identifiers do not refer to a real pending approval.

## Token rotation

The hub accepts a primary token and optional additional tokens:

```txt
TETHON_HUB_AUTH_TOKEN=<primary-token>
TETHON_HUB_AUTH_TOKENS=<additional-token-1>,<additional-token-2>
```

1. Generate a replacement token:

   ```sh
   openssl rand -base64 48
   ```

2. Add it to `TETHON_HUB_AUTH_TOKENS` in Dokploy.
3. Keep the old primary token active.
4. Redeploy the hub.
5. Verify both tokens:

   ```sh
   curl -fsS -H "Authorization: Bearer <old-token>" https://hub.tethon.dev/hosts
   curl -fsS -H "Authorization: Bearer <new-token>" https://hub.tethon.dev/hosts
   ```

6. Replace the [Worker secret](ci-deploy.md#web-worker) and deploy the Worker.
7. Replace the token in each Mac and VPS host configuration.
8. Restart the affected hosts under their [Mac](macos-host-daemon.md) or [VPS](hostd-deploy-dokploy.md) runbook.
9. Replace the token in CLI environments and mobile connection settings.
10. Set the new primary token in Dokploy:

    ```txt
    TETHON_HUB_AUTH_TOKEN=<new-token>
    TETHON_HUB_AUTH_TOKENS=
    ```

11. Redeploy the hub.
12. Verify the final state:

    ```sh
    curl -i -H "Authorization: Bearer <old-token>" https://hub.tethon.dev/hosts
    curl -fsS -H "Authorization: Bearer <new-token>" https://hub.tethon.dev/hosts
    ```

The old token should return `401`. The new token should return `200`.
If a client fails, restore an accepted token before removing the old token.

## Host connectivity

Hosts register and open outbound WebSocket connections to the hub.
Session operations use those connections.
The VPS needs no inbound route to the host diagnostics listener.
Tailscale is optional.

For a session request that returns `502`, inspect both hub and host reverse connection logs.
Useful hub events are `hub_host_registered`, `hub_host_heartbeat`, and `hub_host_heartbeat_unknown`.
An unknown heartbeat can follow a hub restart. The host should register again.
