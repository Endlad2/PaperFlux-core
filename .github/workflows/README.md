# GitHub Actions — Release Pipeline

## Overview

One workflow (`.github/workflows/release.yml`) builds PaperFlux for **four platforms**
in a **matrix**, then creates a single GitHub Release whose tag is the UTC timestamp
`YY.MM.DD.HH.MM`.

## Job graph

```

tag ──┬── android (windows-latest + Android NDK) ──┐
├── ios     (macos-14 + Xcode)             ──┤
├── linux   (ubuntu-latest, matrix amd64/arm64) ──┼── release
└── windows (windows-latest, amd64)         ──┘

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

The script itself builds all four ABIs (`armeabi-v7a`, `arm64-v8a`, `x86`,
`x86_64`) as `libopenflux.so` and drops them in `output/android/<abi>/`.

If you need a different NDK version, change `$ndkVersion` in the workflow and
the matching `rXX` prefix in the download URL.

## iOS

`build_ios.sh` already targets `GOOS=ios`, `GOARCH=arm64`, and builds a
`c-archive` named `liboflux.a` with a hand-written `liboflux.h`. The macOS
runner has the iPhoneOS SDK, so no extra setup is needed.

## Tag format

The tag job uses:

```bash
date -u +'%y.%m.%d.%H.%M'
```

Example: `26.10.13.21.00`. Because the tag has minute precision, two pushes in
the same minute will collide; the release job will fail (the tag already
exists). Wait a minute or use `workflow_dispatch` after the minute rolls over.

## Artifacts produced

| Artifact ↕▾ | Path inside the archive ↕▾ |
|---|---|
| −`paperflux-android.zip` | `android/<abi>/libopenflux.so` |
| −`paperflux-ios.zip` | `liboflux.a`, `liboflux.h` |
| −`paperflux-linux-amd64` | single binary |
| −`paperflux-linux-arm64` | single binary |
| −`paperflux-windows-amd64.exe` | single binary |
⚙

All of them are attached to the release.

## Permissions

The workflow needs `contents: write` (declared at the top) so
`softprops/action-gh-release` can create the release and upload assets using
the default `GITHUB_TOKEN`.

## Manual run

Use **Actions → Release → Run workflow** to trigger a build without pushing.
The tag is still computed from the current UTC time.

