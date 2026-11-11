# Side-by-side test package

Preview APKs must be built with `-PANYTLS_PREVIEW=true`.
The flag changes the **application ID**, not the Kotlin/JNI namespace:

| Distribution | Preview application ID | Normal application ID |
| --- | --- | --- |
| Play Store | `com.v2ray.ang.anytlspreview` | `com.v2ray.ang` |
| F-Droid | `com.v2ray.ang.anytlspreview.fdroid` | `com.v2ray.ang.fdroid` |

The application/foreground-service/tile labels identify the test build as
`v2rayNG AnyTLS TCP TEST` (Chinese locales use 测试版 / 測試版), and the version name
has an `-anytls-tcp-preview` suffix. With the flag absent, existing application IDs,
labels and config-link scheme remain unchanged.

## Package-level checks

- Android assigns separate app-private storage/UIDs to distinct application IDs;
  there is no `sharedUserId`. No original app data is imported or migrated.
- Startup and FileProvider authorities use `${applicationId}`. WorkManager process
  names and internal broadcast actions use `BuildConfig.APPLICATION_ID`.
- Static shortcuts now resolve their target package from the final per-flavor
  resource. They do not hardcode the original Play Store package.
- Preview URL scheme is `v2rayng-anytls-preview`, so it does not register as a
  handler for the original `v2rayng` config links.
- Kotlin classes and JNI bindings remain in `com.v2ray.ang`; changing those names
  would break generated native bindings and is not required for separate app data.

These are package-level protections, **not complete system/network isolation**.
Android normally permits only one active VPN per user; starting another VPN can
replace the active connection. Both apps' local proxy ports can conflict, and
root mode uses system-wide routing tables/chains. Do not run both root/VPN modes
concurrently or change the existing app's configuration as part of this preview.
No phone installation, permission grant or configuration change is automated.

The final APKs still require checks of their merged manifests, labels, provider
and custom-permission names, shortcut resource values, and absence of shared UID.
Source checks alone do not prove installation/runtime coexistence.
