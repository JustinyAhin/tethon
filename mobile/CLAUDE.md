# Tethon mobile

Follow the [root instructions](../AGENTS.md).
This Expo app is the primary client. Use the `[mobile]` commit tag.
See [mobile setup](README.md) for development and build instructions.

## Application rules

- Use Bun. Install dependencies from the repository root.
- Use strict TypeScript and Expo Router under `src/app/`.
- Preserve typed routes and React Compiler in `app.json`.
- Keep `export default function` for route screens and `_layout` files.
- Read the [versioned Expo documentation](https://docs.expo.dev/versions/v56.0.0/) before API changes.
- Keep session state authoritative on the host daemon. Follow the [protocol invariants](../control/CLAUDE.md#protocol-invariants).
- Use shared hub types through `src/lib/hub/types.ts`.
- For shared API changes, follow the [generation and client checks](../control/CLAUDE.md#dependencies-and-schema-changes).
- Keep the notification-response listener at module scope.
- Check the last notification response at startup.
- Keep publishing credentials out of agent sessions. Follow [mobile release operations](../kb/ci-deploy.md#mobile-ota).

## Checks

Run from `mobile/`:

```sh
bun lint
bunx tsc --noEmit
```
