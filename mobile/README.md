# tethon mobile

Expo (React Native) client for tethon — the primary surface for driving coding agents from a phone.

## Develop

```bash
bun install
bun start        # then press i / a / w for iOS / Android / web
```

SDK 56 needs a dev build or a simulator (not Expo Go on a physical device). File-based routing lives in `src/app/`.

## OTA updates

The mobile app is configured for EAS Update through the linked Expo project
`@segbedji/tethon`. The first dogfood phone build should use the release-like
internal `preview` profile:

```bash
eas build --profile preview --platform ios
```

Publish JS, TS, styling, and asset-only changes to the dogfood build with:

```bash
eas update --channel preview --message "<message>" --environment preview
```

The preview app checks for updates on launch. To verify an update, force close
and reopen the installed app up to two times; the downloaded update applies on
the next launch.

Native-affecting changes still require a new EAS build and reinstall. This
includes new native modules, config plugins, bundle identity changes, push
entitlement changes, native project edits, or any change that produces a new
fingerprint runtime version.
