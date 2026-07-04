# Hub Deploy With Dokploy

This is the preferred deployment path when Dokploy already runs on the VPS. The
hub is deployed as a Docker Compose service, and Dokploy handles the public
domain, HTTPS, reverse proxying, logs, and redeploys.

## Shape

```txt
https://hub.tethon.dev
  -> Dokploy domain routing
  -> hub compose service, port 39180
  -> tethon-hub
```

You do not need the Caddy setup for this path. Dokploy already provides the
reverse proxy/domain layer for Compose services.

The hub API still uses `TETHON_HUB_AUTH_TOKEN` bearer auth. Do not put
`hub.tethon.dev` behind Cloudflare Access yet unless the web Worker, host
daemon, and CLI are updated to send Cloudflare Access service-token headers.

Cloudflare DNS proxying is fine. Cloudflare Access is the part to avoid for the
hub for now.

## Files

- `deploy/hub/Dockerfile` - builds the `tethon-hub` binary into a small image.
- `deploy/hub/docker-compose.yml` - Dokploy Compose service with the persistent
  `hub-data` volume mounted at `/var/lib/tethon`.

## Dokploy Setup

1. Point DNS at the VPS.

   Create an `A` record:

   ```txt
   hub.tethon.dev -> <vps-ipv4>
   ```

   Add an `AAAA` record too if the VPS has IPv6.

2. Create a Dokploy project/service.

   In Dokploy:

   - Create or open the Tethon project.
   - Add a new `Compose` service.
   - Choose `Docker Compose`, not `Stack`.
   - Set the source to this GitHub repo.
   - Set the branch to `main`.
   - Set the Compose Path to:

   ```txt
   deploy/hub/docker-compose.yml
   ```

3. Set the hub token.

   In the Compose service environment variables, add:

   ```txt
   TETHON_HUB_AUTH_TOKEN=<strong-random-token>
   ```

   Use the same token for:

   - this Dokploy environment variable
   - the `HUB_AUTH_TOKEN` Worker secret on `tethon-web`
   - local `TETHON_HUB_AUTH_TOKEN` when running `tethon-hostd` or `tethon`

   The compose file references the variable explicitly:

   ```yaml
   environment:
     TETHON_HUB_AUTH_TOKEN: ${TETHON_HUB_AUTH_TOKEN:?set TETHON_HUB_AUTH_TOKEN in Dokploy}
     TETHON_HUB_AUTH_TOKENS: ${TETHON_HUB_AUTH_TOKENS:-}
   volumes:
     - hub-data:/var/lib/tethon
   ```

   `TETHON_HUB_AUTH_TOKENS` is optional. It is only needed during token
   rotation.

   The hub stores registered mobile push devices in SQLite at
   `/var/lib/tethon/hub.db`. Keep the `hub-data` volume attached across
   redeploys so phones do not need to reopen the app after every hub restart.

4. Configure the domain in Dokploy.

   Use Dokploy's Domains tab for the Compose service:

   - Domain: `hub.tethon.dev`
   - Service: `hub`
   - Container port: `39180`
   - HTTPS: enabled

   Do not add manual Traefik labels to the compose file unless Dokploy's native
   Domains UI cannot work for your VPS setup.

5. Deploy.

   Click Deploy in Dokploy and watch the deployment logs. The image build should
   compile `./cmd/tethon-hub`, then start the container with:

   ```sh
   tethon-hub -addr 0.0.0.0:39180
   ```

   For runtime diagnostics, use the Dokploy service logs for the Compose
   service. Useful hub log events:

   - `hub_host_registered`: a host registered or refreshed its metadata. Check
     `host_id`, `url`, `repo_count`, `agent_count`, and `online`.
   - `hub_host_heartbeat`: a registered host heartbeat succeeded. Check
     `host_id`, `url`, and `last_seen`.
   - `hub_host_heartbeat_unknown`: a daemon sent a heartbeat for a host id the
     in-memory hub registry does not know, usually after a hub restart; hostd
     should re-register on the next loop.

6. Smoke test.

   From your machine:

   ```sh
   curl -fsS https://hub.tethon.dev/healthz
   ```

   `/healthz` is intentionally unauthenticated and cheap. It only confirms the
   hub process and reverse proxy path are alive; it does not fan out to hosts or
   require the bearer token.

   The authenticated API should reject missing auth:

   ```sh
   curl -i https://hub.tethon.dev/hosts
   ```

   It should return `401 Unauthorized`.

   With the token:

   ```sh
   export TETHON_HUB_AUTH_TOKEN='<same-token>'
   curl -fsS -H "Authorization: Bearer $TETHON_HUB_AUTH_TOKEN" https://hub.tethon.dev/hosts
   ```

7. Point web at the hub.

   The `tethon-web` Worker needs:

   ```txt
   HUB_BASE_URL=https://hub.tethon.dev
   HUB_AUTH_TOKEN=<same token as TETHON_HUB_AUTH_TOKEN>
   ```

   `HUB_AUTH_TOKEN` should be a Worker secret.

## Token Rotation

The hub accepts one primary token plus optional additional accepted tokens:

```txt
TETHON_HUB_AUTH_TOKEN=<primary-token>
TETHON_HUB_AUTH_TOKENS=<additional-token-1>,<additional-token-2>
```

Use this to rotate without downtime:

1. Generate a new token:

   ```sh
   openssl rand -base64 48
   ```

2. In Dokploy, add the new token to `TETHON_HUB_AUTH_TOKENS`, leaving the old
   `TETHON_HUB_AUTH_TOKEN` in place. Deploy the hub.

3. Verify both old and new tokens work:

   ```sh
   curl -fsS -H "Authorization: Bearer <old-token>" https://hub.tethon.dev/hosts
   curl -fsS -H "Authorization: Bearer <new-token>" https://hub.tethon.dev/hosts
   ```

4. Update the `tethon-web` Worker secret:

   ```sh
   cd web
   bun wrangler secret put HUB_AUTH_TOKEN
   bun deploy
   ```

5. Update each installed Mac host daemon config:

   ```txt
   ~/.config/tethon/hostd.env
   ```

   Set:

   ```sh
   TETHON_HUB_AUTH_TOKEN=<new-token>
   ```

   Restart the daemon:

   ```sh
   scripts/macos-hostd.sh restart
   ```

6. Update any local CLI shell/env setup that exports `TETHON_HUB_AUTH_TOKEN`.

7. Promote the new token in Dokploy:

   ```txt
   TETHON_HUB_AUTH_TOKEN=<new-token>
   TETHON_HUB_AUTH_TOKENS=
   ```

   Deploy the hub again.

8. Confirm the old token fails and the new token still works:

   ```sh
   curl -i -H "Authorization: Bearer <old-token>" https://hub.tethon.dev/hosts
   curl -fsS -H "Authorization: Bearer <new-token>" https://hub.tethon.dev/hosts
   ```

   The old token should return `401 Unauthorized`.

For local development, dotenvx can be used as an optional convenience to load
`TETHON_HUB_AUTH_TOKEN` for commands:

```sh
dotenvx run -- go run ./control/cmd/tethon hosts list --hub-url https://hub.tethon.dev
```

Do not use dotenvx for Dokploy or Cloudflare production secrets.

## Current Host Connectivity Caveat

The current prototype still has the hub call the host daemon at the URL the
daemon registers. Until host-initiated persistent connections land, the VPS must
be able to reach each `tethon-hostd`.

For your own setup now, use Tailscale on both the VPS and Mac:

```sh
tethon-hostd \
  -hub-url https://hub.tethon.dev \
  -addr <mac-tailscale-ip>:39181 \
  -repo-root /path/to/repos
```

The daemon will register `http://<mac-tailscale-ip>:39181`, and the hub on the
VPS can route to it.

If hosts register but session listing or session creation returns `502`, check
the Dokploy hub logs and confirm the VPS can call the registered `url` directly,
for example over Tailscale. If `/hosts` returns `401`, the hub, web Worker,
host daemon, or CLI are not using the same accepted bearer token.
