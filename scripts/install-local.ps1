# Builds the provider and installs it into the local plugin directory that
# Terraform and OpenTofu search before the registry, so `init` uses this build.
#
#   scripts/install-local.ps1 [-Version <version>]    # version defaults to 0.0.1
#
# Rebuilding the same version changes its checksum, which the lock file of an
# already initialised configuration rejects (even with `init -upgrade`).
# Delete .terraform.lock.hcl there and rerun `tofu init`, or install under a
# new version.
param(
    [string]$Version = "0.0.1"
)

$ErrorActionPreference = "Stop"

$Version = $Version -replace '^v', ''

$os = go env GOOS
$arch = go env GOARCH
$ext = go env GOEXE

if ($os -eq "windows") {
    $plugins = Join-Path $env:APPDATA "terraform.d/plugins"
} else {
    $plugins = Join-Path $HOME ".terraform.d/plugins"
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $binary = "terraform-provider-mamori_v$Version$ext"
    Push-Location (Join-Path $PSScriptRoot "..")
    try {
        go build -trimpath -ldflags "-X main.version=$Version" -o (Join-Path $tmp $binary) .
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally {
        Pop-Location
    }

    # OpenTofu resolves "mamori-io/mamori" to registry.opentofu.org and
    # Terraform to registry.terraform.io, so install under both hostnames.
    foreach ($hostname in "registry.opentofu.org", "registry.terraform.io") {
        $dir = Join-Path $plugins "$hostname/mamori-io/mamori/$Version/${os}_$arch"
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        Copy-Item (Join-Path $tmp $binary) $dir -Force
        Write-Output "Installed $(Join-Path $dir $binary)"
    }
} finally {
    Remove-Item -Recurse -Force $tmp
}
