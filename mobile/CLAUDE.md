# tethon mobile

Expo (React Native) client — the **primary** surface for tethon: drive coding agents from a phone with a structured-first UI (approval cards, diff viewer, agent stream). The SvelteKit web companion is the thinner secondary screen; this is where the real UX lives. Per the build order (`../kb/agent-control-design.md`), structured views come before a raw terminal.

General repo rules (git, commit tags, issue tracking, working principles, never reading `.env`) live in the root `CLAUDE.md`. This file covers mobile-specific rules only. Commit with the `[mobile]` tag.

## Tech Stack

- **Package manager / runtime**: Bun (not npm/pnpm/yarn). Install dependencies from the repo root with `bun install`.
- **Framework**: Expo SDK 56, React Native 0.85, React 19.
- **Routing**: expo-router (file-based) under `src/app/`. Typed routes are on; React Compiler is enabled.
- **Language**: TypeScript, `strict` mode. `@/*` maps to `src/*`, `@/assets/*` to `assets/*`.
- **Builds**: SDK 56 needs a dev build or a simulator — Expo Go on a physical device won't run it.

## Documentation

Expo and React Native change fast. **Read the versioned docs before writing code:** https://docs.expo.dev/versions/v56.0.0/. Don't rely on memory of older APIs.

## Code conventions

Mirror the web app for cross-codebase consistency:

- Use `type`, not `interface`.
- Arrow functions, not function declarations — **except** route screens and `_layout` files, which expo-router requires as `export default function`.
- Functions with more than one parameter take a single object argument.
- Name component files in kebab-case (e.g. `approval-card.tsx`); import under a PascalCase identifier.

## Session state

The hub is the source of truth for session state — the client reads and relays the event log, it does not own it. Mirror the protocol invariants in `../control/CLAUDE.md` (append-only events, cursor replay, reconnect-with-last-cursor); don't invent client-side state that diverges from the log.

Hub protocol types come from `@tethon/hub-sdk` and are re-exported through `src/lib/hub/types.ts`. Regenerate them from the repo root with `bun run gen:protocol`.

## Commands

```bash
bun install      # from repo root
bun start        # dev server; press i / a / w for iOS / Android / web
bun ios          # iOS simulator
bun android      # Android emulator
bun web          # web build
bun lint         # expo lint
bunx tsc --noEmit  # type check
```

Kill any dev server you start before finishing.
