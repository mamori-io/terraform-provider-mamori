# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Terraform/OpenTofu provider for mamori.io servers (module path `mamori/terraform-provider`, Go 1.27.1, registry address `registry.terraform.io/mamori-io/mamori` set in `main.go`).

It is built on the Terraform Plugin Framework (not SDKv2) and talks to the server through `mamori.io/mamori-go-client`. That import path is redirected by a `replace` directive to `github.com/mamori-io/mamori-go-client`; keep the `replace` when bumping the client. The client's source is in the module cache (`go env GOMODCACHE`), and a sibling checkout lives at `../mamori-go-client`.

## Commands

```sh
go build ./...
go vet ./...
go test ./...
go test ./internal/provider -run TestPermissionMatches   # single test
```

The tests are unit tests (schema validity, permission mapping/matching) and need no server.

To try the provider with OpenTofu (`tofu` is installed) without publishing it, build the binary into a directory and point a CLI config's `dev_overrides` at it:

```hcl
provider_installation {
  dev_overrides { "mamori-io/mamori" = "<dir containing terraform-provider-mamori.exe>" }
  direct {}
}
```

Then run `TF_CLI_CONFIG_FILE=<that file> tofu validate` (or `plan`) in a directory with a config such as `examples/main.tf`. Skip `tofu init` with dev overrides. `plan`/`apply` need a reachable server; credentials come from the provider block or `MAMORI_SERVER`, `MAMORI_USERNAME`, `MAMORI_PASSWORD`, `MAMORI_INSECURE_SKIP_VERIFY`.

## Architecture

Everything is in `internal/provider`:

- `provider.go` logs in once in `Configure` and passes the session-holding `*mamori.Client` to every resource. New resources must be added to `Resources()`.
- Each resource embeds `clientResource` (`helpers.go`), which provides `Configure`. Not-found detection is `isNotFound` (`mamori.ErrNotFound` or HTTP 404); `Read` removes the resource from state in that case.
- Resources are keyed by name, not server id: the name attribute is `RequiresReplace` and is the import ID. Where the client's update calls need a numeric server id (secrets, HTTP resources, SSH and remote desktop logins), `Create` looks the object up by name afterwards and stores it in `id`.
- `mamori_role_grant` and `mamori_permission` are grant/revoke resources. Every attribute forces replacement, and `Update` only copies the plan.
- `mamori_permission` maps its `type` attribute onto the client's concrete `Permission` types (`permissionModel.permission`). `Read` lists the grantee's permissions, decodes each with `mamori.PermissionFromRecord`, and matches them loosely (`permissionMatches`). The server's record shapes vary by type, and policy grants can't be decoded at all, so policy grants are assumed present.

Known limits come from the server API rather than from this code. Keep them in mind before tightening `Read`:
- Secret values and SSH, remote desktop and datasource passwords are never returned, so drift in them is not detected.
- `mamori_datasource` `Read` only checks existence. The client sends empty strings as "no change", so removing a datasource setting from config does not clear it on the server.
- `mamori_user.password` is only applied at creation.
