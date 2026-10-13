# GitHub Actions — Release Pipeline

## Overview

One workflow (`.github/workflows/release.yml`) builds PaperFlux for **five platforms**
in a **matrix**, then creates a single GitHub Release whose tag is the UTC timestamp
`YY.MM.DD.HH.MM`.

## Job graph

```

tag ──┬── android (windows-latest + Android NDK) ────────┐
├── ios     (macos-14 + Xcode)                    ──┤
├── linux   (ubuntu-latest, matrix amd64/arm64)   ──┼── release
├── windows (windows-latest, amd64)               ──┤
└── openwrt (ubuntu-latest, OpenWRT SDK 25.12.5)  ──┘

```

The `tag` job produces the shared release tag that every build job stamps into
the binary via `-ldflags "-X main.buildVersion=<tag>"` (and into the Android
build with `-Version <tag>`).

## Why a Windows runner for Android

`scripts/build-android-native.ps1` is PowerShell and uses the **Windows** NDK
toolchain path (`toolchains/llvm/prebuilt/windows-x86_64/bin`). The job:

1. Downloads `android-ndk-r27-windows.zip` from `dl.google.com`.
2. Extracts it to `C:\Android\Sdk\ndk\27.0.12077973`.
3. Runs `scripts/build-android-native.ps1 -NdkPath <dir>`.

The script builds all four ABIs (`armeabi-v7a`, `arm64-v8a`, `x86`, `x86_64`)
as `libopenflux.so` and drops them in `output/android/<abi>/`.

## iOS

`build_ios.sh` already targets `GOOS=ios`, `GOARCH=arm64`, and builds a
`c-archive` named `liboflux.a` with a hand-written `liboflux.h`.

## OpenWRT

The OpenWRT job uses the official SDK:

| Field | Value |
| --- | --- |
| SDK | `openwrt-sdk-25.12.5-armsr-armv8_gcc-14.3.0_musl.Linux-x86_64` |
| Target | `armsr/armv8` (aarch64, musl) |
| URL | `https://downloads.openwrt.org/releases/25.12.5/targets/armsr/armv8/` |
| SHA256 | `1b0316604a3e820b2b008a1baff3f9dac6716af942bef800930e58c7de98c98b` |

`scripts/fetch-openwrt-sdk.sh` performs the download, verifies the SHA256, and
extracts the SDK into `<repo_root>/.cache/openwrt-sdk` (overridable via the
first positional argument). The workflow caches that directory across runs.

The build:
- `GOOS=linux`, `GOARCH=arm64`, `CGO_ENABLED=1`
- `CC` is auto-discovered from `staging_dir/toolchain-*/bin/aarch64-openwrt-linux-musl-gcc`
- `STAGING_DIR` points at the SDK's `staging_dir`

Result: `paperflux-openwrt.zip` containing `paperflux-openwrt` and a short
`README-openwrt.txt` describing the target.

## Tag format

The tag job uses:

```bash
date -u +'%y.%m.%d.%H.%M'
```

Example: `26.10.13.21.00`. Because the tag has minute precision, two pushes in
the same minute will collide; the release job will fail (the tag already
exists). Wait a minute or use `workflow_dispatch` after the minute rolls over.

## Artifacts produced

| Artifact ↕▾ | Contents ↕▾ |
|---|---|
| −`paperflux-android.zip` | `android/<abi>/libopenflux.so` |
| −`paperflux-ios.zip` | `liboflux.a`, `liboflux.h` |
| −`paperflux-linux-amd64` | single binary |
| −`paperflux-linux-arm64` | single binary |
| −`paperflux-windows-amd64.exe` | single binary |
| −`paperflux-openwrt.zip` | `paperflux-openwrt`, `README-openwrt.txt` |
⚙

All of them are attached to the release.

## Permissions

The workflow needs `contents: write` (declared at the top) so
`softprops/action-gh-release` can create the release and upload assets using
the default `GITHUB_TOKEN`.

## Manual run

Use **Actions → Release → Run workflow** to trigger a build without pushing.
The tag is still computed from the current UTC time.

## Reusing the SDK fetcher locally

```
scripts/fetch-openwrt-sdk.sh              # into ./.cache/openwrt-sdk
scripts/fetch-openwrt-sdk.sh /tmp/owrt-sdk  # custom destination
```

The script prints `sdk_root=<path>` on success, which you can consume with
`eval "$(scripts/fetch-openwrt-sdk.sh)"`.

