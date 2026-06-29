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
- `deploy/hub/docker-compose.yml` - Dokploy Compose service.

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
   ```

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

6. Smoke test.

   From your machine:

   ```sh
   curl -fsS https://hub.tethon.dev/healthz
   ```

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
