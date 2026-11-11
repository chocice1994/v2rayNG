#!/usr/bin/env bash
# Build a single Go/JNI runtime, with all four app-supported Android ABIs.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
if [[ $# != 1 ]]; then echo "Usage: $0 NEW_ABSOLUTE_OUTPUT_DIRECTORY" >&2; exit 2; fi
[[ "$1" == /* ]] || { echo "Output must be absolute" >&2; exit 2; }
command -v go >/dev/null
# /usr/bin/go can be an unrelated desktop game. Fail clearly in that case.
go version | grep -q '^go version go' || { echo "A Go compiler is required" >&2; exit 2; }
: "${ANDROID_HOME:?Set ANDROID_HOME to the Android SDK}"
: "${ANDROID_NDK_HOME:?Set ANDROID_NDK_HOME to the Android NDK}"
python3 "$root/native/prepare_anytls_core.py" "$1"
cd "$1"
# An archived overlay intentionally has no VCS metadata. Do not let Go inspect
# an unrelated ancestor worktree (or fail on restricted ancestor .git files).
export GOFLAGS="${GOFLAGS:+$GOFLAGS }-buildvcs=false -p=${ANYTLS_GO_PARALLELISM:-2}"
export GOMAXPROCS="${GOMAXPROCS:-2}"
export TMPDIR="${ANYTLS_TMPDIR:-$1/tmp}"
export GOTMPDIR="$TMPDIR"
mkdir -p "$TMPDIR"
go mod tidy
go test -race -count=20 -timeout=120s ./internal/anytlsbridge
go test -race -count=3 -timeout=120s ./
go test -tags=purego -race -count=3 -timeout=120s ./...
(cd internal/third_party/sing-anytls && go test -race -count=20 -timeout=120s ./...)
go vet ./...
mobile_version="$(go list -m -f '{{.Version}}' golang.org/x/mobile)"
GOBIN="$1/tools" go install "golang.org/x/mobile/cmd/gomobile@${mobile_version}"
GOBIN="$1/tools" go install "golang.org/x/mobile/cmd/gobind@${mobile_version}"
export PATH="$1/tools:$PATH"
gomobile init
# Use the checkout's source data workflow rather than stale generated AARs.
mkdir -p assets data
bash gen_assets.sh download
cp data/*.dat assets/
# gomobile internally compiles target ABIs concurrently. Bind them sequentially
# for bounded memory/temp-space usage, then verify identical common Java/assets.
for target in arm arm64 386 amd64; do
    compatibility_flags=()
    if [[ "$target" == amd64 ]]; then
        # Go 1.27 + x/crypto v0.58.0 blake2b assembly has non-PIC SSE/AVX
        # relocations in Android/amd64 shared builds. Use its official purego
        # path for this ABI only. Remove after a pinned upstream combination
        # links without R_X86_64_PC32 and passes the same ABI/integration checks.
        compatibility_flags=(-tags=purego)
    fi
    gomobile bind "${compatibility_flags[@]}" -target="android/$target" -androidapi 24 -trimpath -ldflags='-s -w -buildid= -checklinkname=0' -o "libv2ray-$target.aar" ./
done
python3 "$root/native/merge_aar.py" libv2ray.aar libv2ray-arm.aar libv2ray-arm64.aar libv2ray-386.aar libv2ray-amd64.aar
python3 - <<'PY'
import zipfile
with zipfile.ZipFile('libv2ray.aar') as archive:
    for abi in ('armeabi-v7a', 'arm64-v8a', 'x86', 'x86_64'):
        assert f'jni/{abi}/libgojni.so' in archive.namelist(), f'Missing {abi}'
print('Verified libgojni.so for all four ABIs')
PY
printf 'Built %s/libv2ray.aar\n' "$1"
