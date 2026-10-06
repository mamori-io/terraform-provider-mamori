#!/usr/bin/env bash
# Builds the provider and installs it into the local plugin directory that
# Terraform and OpenTofu search before the registry, so `init` uses this build.
#
#   scripts/install-local.sh [version]    # version defaults to 0.0.1
#
# Rebuilding the same version changes its checksum, which the lock file of an
# already initialised configuration rejects (even with `init -upgrade`).
# Delete .terraform.lock.hcl there and rerun `tofu init`, or install under a
# new version.
set -euo pipefail

version="${1:-0.0.1}"
version="${version#v}"

os="$(go env GOOS)"
arch="$(go env GOARCH)"
ext="$(go env GOEXE)"

if [ "$os" = windows ]; then
  plugins="$(cygpath -u "$APPDATA" 2>/dev/null || echo "$APPDATA")/terraform.d/plugins"
else
  plugins="$HOME/.terraform.d/plugins"
fi

cd "$(dirname "$0")/.."

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
binary="terraform-provider-mamori_v${version}${ext}"
go build -trimpath -ldflags "-X main.version=${version}" -o "$tmp/$binary" .

# OpenTofu resolves "mamori-io/mamori" to registry.opentofu.org and Terraform
# to registry.terraform.io, so install under both hostnames.
for host in registry.opentofu.org registry.terraform.io; do
  dir="$plugins/$host/mamori-io/mamori/$version/${os}_${arch}"
  mkdir -p "$dir"
  cp "$tmp/$binary" "$dir/"
  echo "Installed $dir/$binary"
done
