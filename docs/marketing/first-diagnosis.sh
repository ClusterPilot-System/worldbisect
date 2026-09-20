#!/usr/bin/env bash
# A controlled first result using the checksum-pinned 1.2.0 release and local fixture.
set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
[[ "$(uname -s)" == Linux ]] || { echo 'Run this demo on Linux or WSL.' >&2; exit 1; }
for required in curl sha256sum tar grep timeout; do
  command -v "$required" >/dev/null || { echo "Missing required command: $required" >&2; exit 1; }
done
case "$(uname -m)" in
  x86_64)
    arch=amd64
    expected=632370e3d3b02b31912c252d6d24d01a4f788f6f350cf8c84876f44d51c0615e
    ;;
  aarch64|arm64)
    arch=arm64
    expected=188c719bd231e7236280a442ea621521077672ace317f4c8f5a39abd55459352
    ;;
  *) echo "Unsupported Linux architecture: $(uname -m)" >&2; exit 1 ;;
esac

demo_dir=$(mktemp -d)
trap 'rm -rf "$demo_dir"' EXIT
started=$SECONDS
archive="worldbisect_1.2.0_linux_${arch}.tar.gz"
printf 'Downloading and verifying WorldBisect 1.2.0 for Linux %s...\n' "$arch"
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
  --connect-timeout 15 --max-time 120 \
  "https://github.com/ClusterPilot-System/worldbisect/releases/download/v1.2.0/$archive" \
  --output "$demo_dir/$archive"
printf '%s  %s\n' "$expected" "$demo_dir/$archive" | sha256sum --check --status -
tar --extract --no-same-owner --file "$demo_dir/$archive" --directory "$demo_dir"
binary="$demo_dir/worldbisect_1.2.0_linux_${arch}/bin/worldbisect"
"$binary" version

cp -R "$repo_root/examples/file-cause" "$demo_dir/good"
cp -R "$repo_root/examples/file-cause" "$demo_dir/bad"
cp "$demo_dir/good/config.good.txt" "$demo_dir/good/config.txt"
cp "$demo_dir/bad/config.bad.txt" "$demo_dir/bad/config.txt"
mkdir "$demo_dir/home"

printf '\nControlled inputs: good/config.txt = mode=good; bad/config.txt = mode=bad\n'
printf 'Capturing both checks with portable capture (--trace off)...\n'
clean_env=(env -i "PATH=$PATH" LANG=C "HOME=$demo_dir/home")
"${clean_env[@]}" "$binary" capture --trace off --timeout 10s \
  --store "$demo_dir/store" --workspace "$demo_dir/good" \
  --oracle exit=0 --output "$demo_dir/good.wcap" -- ./check.sh > "$demo_dir/good.out"
if "${clean_env[@]}" "$binary" capture --trace off --timeout 10s \
  --store "$demo_dir/store" --workspace "$demo_dir/bad" \
  --oracle exit=0 --output "$demo_dir/bad.wcap" -- ./check.sh \
  > "$demo_dir/bad.out" 2> "$demo_dir/bad.err"; then
  echo 'The deliberately bad fixture unexpectedly passed.' >&2
  exit 1
fi
[[ -s "$demo_dir/bad.wcap" ]] || { cat "$demo_dir/bad.err" >&2; exit 1; }

printf '\nTesting both directions and repeated evidence checks...\n\n'
"${clean_env[@]}" timeout --foreground 60s "$binary" compare \
  --store "$demo_dir/store" --good "$demo_dir/good.wcap" --bad "$demo_dir/bad.wcap" \
  --repetitions 3 --max-experiments 32 --format markdown -- ./check.sh \
  > "$demo_dir/diagnosis.md"
cat "$demo_dir/diagnosis.md"
if ! grep -Fq '**Status:** `PROVEN`' "$demo_dir/diagnosis.md"; then
  echo 'The controlled fixture did not earn PROVEN. Keep the actual result when reporting it.' >&2
  exit 1
fi
if ! grep -Fq 'workspace file "config.txt"' "$demo_dir/diagnosis.md"; then
  echo 'The expected config.txt factor was not reported.' >&2
  exit 1
fi
printf '\nDemo completed in %s seconds including download; your timing will vary.\n' "$((SECONDS - started))"
printf 'Temporary captures and the downloaded binary are removed on exit.\n'
