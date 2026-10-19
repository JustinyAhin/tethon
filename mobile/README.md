# Tethon mobile

Use the mobile app to control agent sessions from a phone.
It uses Expo and React Native.

## Develop

Install dependencies from the repository root:

```sh
bun install
```

Start the app from `mobile/`:

```sh
bun start
```

Press `i`, `a`, or `w` for iOS, Android, or web.
Use a development build or simulator with Expo SDK 56.
Expo Go on a physical device is not supported by this setup.
Routes are in `src/app/`.

See [component rules](CLAUDE.md) for checks.
See [mobile release operations](../kb/ci-deploy.md#mobile-ota) for preview builds, updates, and verification.
