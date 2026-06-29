# Hub Deploy With Caddy

This is the current deployment path for `hub.tethon.dev`: a small VPS runs the
Go hub binary on localhost, and Caddy terminates HTTPS and proxies public
traffic to it.

## Shape

```txt
https://hub.tethon.dev
  -> Caddy on the VPS
  -> 127.0.0.1:39180
  -> tethon-hub
```

The hub API uses `TETHON_HUB_AUTH_TOKEN` bearer auth. Do not put
`hub.tethon.dev` behind Cloudflare Access yet unless the web Worker, host
daemon, and CLI are updated to send Cloudflare Access service-token headers.

Cloudflare proxying is fine. Cloudflare Access is the part to avoid for the hub
for now.

## Files

- `deploy/hub/Caddyfile` - Caddy site block for `hub.tethon.dev`.
- `deploy/hub/tethon-hub.service` - systemd service for the hub binary.

## VPS Setup

These commands assume Ubuntu/Debian and a VPS user with sudo access.

1. Point DNS at the VPS.

   Create an `A` record:

   ```txt
   hub.tethon.dev -> <vps-ipv4>
   ```

   Add an `AAAA` record too if the VPS has IPv6.

2. Open inbound web ports on the VPS firewall.

   ```sh
   sudo ufw allow OpenSSH
   sudo ufw allow 80/tcp
   sudo ufw allow 443/tcp
   sudo ufw enable
   sudo ufw status
   ```

3. Install Caddy.

   Official Debian/Ubuntu packages use Caddy's Cloudsmith apt repository:

   ```sh
   sudo apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | sudo gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
   curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | sudo tee /etc/apt/sources.list.d/caddy-stable.list
   sudo chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg
   sudo chmod o+r /etc/apt/sources.list.d/caddy-stable.list
   sudo apt update
   sudo apt install -y caddy
   ```

   Verify:

   ```sh
   caddy version
   systemctl status caddy --no-pager
   ```

4. Create the service user and config directory.

   ```sh
   sudo useradd --system --home /var/lib/tethon --create-home --shell /usr/sbin/nologin tethon
   sudo install -d -m 0750 -o root -g tethon /etc/tethon
   ```

5. Create the hub env file.

   Generate one strong token and use the same value for:

   - the VPS env file below
   - the `HUB_AUTH_TOKEN` Worker secret on `tethon-web`
   - local `TETHON_HUB_AUTH_TOKEN` when running `tethon-hostd` or `tethon`

   ```sh
   sudoedit /etc/tethon/hub.env
   ```

   Contents:

   ```sh
   TETHON_HUB_AUTH_TOKEN=<strong-random-token>
   ```

   Lock it down:

   ```sh
   sudo chown root:tethon /etc/tethon/hub.env
   sudo chmod 0640 /etc/tethon/hub.env
   ```

6. Install the hub binary.

   Build for the VPS architecture locally from the repo root:

   ```sh
   cd control
   mkdir -p dist
   GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/tethon-hub-linux-amd64 ./cmd/tethon-hub
   cd ..
   ```

   For ARM VPS:

   ```sh
   cd control
   mkdir -p dist
   GOOS=linux GOARCH=arm64 go build -trimpath -ldflags='-s -w' -o dist/tethon-hub-linux-arm64 ./cmd/tethon-hub
   cd ..
   ```

   Copy it to the VPS:

   ```sh
   scp control/dist/tethon-hub-linux-amd64 <vps-user>@<vps-host>:/tmp/tethon-hub
   ```

   Install it on the VPS:

   ```sh
   sudo install -m 0755 -o root -g root /tmp/tethon-hub /usr/local/bin/tethon-hub
   ```

7. Install the systemd unit.

   From the repo on your local machine:

   ```sh
   scp deploy/hub/tethon-hub.service <vps-user>@<vps-host>:/tmp/tethon-hub.service
   ```

   On the VPS:

   ```sh
   sudo install -m 0644 -o root -g root /tmp/tethon-hub.service /etc/systemd/system/tethon-hub.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now tethon-hub
   sudo systemctl status tethon-hub --no-pager
   ```

8. Install the Caddy site.

   From the repo on your local machine:

   ```sh
   scp deploy/hub/Caddyfile <vps-user>@<vps-host>:/tmp/tethon-hub.Caddyfile
   ```

   On the VPS, either replace `/etc/caddy/Caddyfile` with this file, or import
   it from your main Caddyfile.

   Simple replacement:

   ```sh
   sudo install -m 0644 -o root -g root /tmp/tethon-hub.Caddyfile /etc/caddy/Caddyfile
   sudo caddy validate --config /etc/caddy/Caddyfile
   sudo systemctl reload caddy
   ```

9. Smoke test from the VPS.

   ```sh
   curl -fsS http://127.0.0.1:39180/healthz
   curl -fsS https://hub.tethon.dev/healthz
   ```

   The authenticated API should reject missing auth:

   ```sh
   curl -i https://hub.tethon.dev/hosts
   ```

   It should return `401 Unauthorized`.

   With the token:

   ```sh
   . /etc/tethon/hub.env
   curl -fsS -H "Authorization: Bearer $TETHON_HUB_AUTH_TOKEN" https://hub.tethon.dev/hosts
   ```

10. Point web at the hub.

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
