# Tethon

Tethon runs coding agents across repositories and host machines.
The mobile app is primary. The web app is a companion.

Read the [design brief](kb/agent-control-design.md) before architectural changes.
Treat it as target design unless it explicitly describes current behavior.
Follow the [Go](control/CLAUDE.md), [web](web/CLAUDE.md), and [mobile](mobile/CLAUDE.md) rules for component work.

Use ASD-STE100 Issue 9 for English responses and maintained documentation.
Keep documentation small. Update the existing topic owner. Do not repeat rules.
Keep only useful, project-specific documentation and evidence.
Link to shared runbooks instead of copying generic guidance.
Use short sentences, active voice, and one instruction per sentence.
Preserve existing user changes. Never read `.env` or other secret files.
Describe required environment variables and let the user set them.

Follow the shared [workflow](https://github.com/JustinyAhin/kb/blob/main/engineering/workflow.md),
[code conventions](https://github.com/JustinyAhin/kb/blob/main/engineering/code-conventions.md),
and [writing policy](https://github.com/JustinyAhin/kb/blob/main/engineering/documentation.md#writing-policy).
Add tests only for meaningful failure risks that existing checks cannot cover.
Report only checks that you did. Stop temporary development servers before finishing.

## Git and issues

Do not commit or push without an explicit request.
Keep commit messages short. Do not add co-author or agent trailers.
Use one commit tag: `[proto]`, `[daemon]`, `[hub]`, `[web]`, `[mobile]`, `[feat]`, `[fix]`, `[infra]`, or `[chore]`.

Use Beads (`bd`, prefix `tethon`) under the [shared workflow and backup runbook](https://github.com/JustinyAhin/kb/blob/main/engineering/issue-tracking.md).
Close issues only when explicitly asked. Run backups only when explicitly asked.
When committing Beads work, include related changes to `.beads/interactions.jsonl`. Do not include unrelated entries.

## Runtime changes

Run the component checks before handing back changes.
For protocol or hub API changes, also follow the generation and client checks in the [Go rules](control/CLAUDE.md#dependencies-and-schema-changes).
Tell the user exactly which runtime still needs a restart or deployment.
A code change or commit does not update a running service.

- Host daemon: changes to `control/cmd/tethon-hostd`, `control/internal/server`, `control/internal/agent`,
  `control/internal/repo`, `control/internal/worktree`, or `scripts/macos-hostd.sh` need Go checks from `control/`.
  Tell the user to reinstall with `scripts/macos-hostd.sh install ...same args...`.
  Then tell them to run `scripts/macos-hostd.sh restart`.
- Hub: changes to `control/cmd/tethon-hub`, `control/internal/hub`, or `deploy/hub/` need Go checks from `control/`.
  Also run `TETHON_HUB_AUTH_TOKEN=dummy docker compose -f deploy/hub/docker-compose.yml config` from the repository root.
  Tell the user to redeploy the hub in Dokploy.
- Web: changes to `web/` need `bun lint` and `bun check` from `web/`.
  Tell the user that the Worker needs deployment or promotion to update `web.tethon.dev`.

Documentation-only changes need no runtime restart or deployment.

## Source readability

Use blank lines to separate import groups, types, state, derived values, helpers, and exports.
Separate validation, database work, and results in longer functions.
Expand complex markup blocks. Leave space between major page sections and CSS rules.
Keep related short statements together. Add comments to explain reasons that the code does not show.
Keep visible text spacing intact. Do not edit generated files or vendor files for layout.
