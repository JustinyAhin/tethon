# Hostd Dokploy deploy stub

This is the minimal VPS hostd bring-up note for `deploy/hostd/`. The full
runbook polish belongs with ttb.5.

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
prompts. Use Contents read/write for the tethon repo so the later dogfood
session can push.

## Verify

After the next restart, check hub logs for `hub_host_registered` with `vps-1`
and reverse WebSocket connection logs. On the hostd container:

```bash
curl -fsS http://127.0.0.1:39181/status
```

The response should show host id `vps-1`, repo `tethon`, and `claude-cli` in
the agent list. Stop there; starting a real dogfood session is ttb.3.
