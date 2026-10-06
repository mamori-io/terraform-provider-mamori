# Terraform Provider for mamori.io

A Terraform and OpenTofu provider for managing [mamori.io](https://mamori.io) servers: users, roles, datasources, resources, stored logins, secrets and the permissions that tie them together.

It is built on the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework) and uses [mamori-go-client](https://github.com/mamori-io/mamori-go-client) to talk to the server.

## Usage

```hcl
terraform {
  required_providers {
    mamori = {
      source = "mamori-io/mamori"
    }
  }
}

provider "mamori" {
  server               = "https://mamori.example.com"
  insecure_skip_verify = true
}

resource "mamori_role" "analysts" {
  name = "analysts"
}

resource "mamori_user" "alice" {
  username = "alice"
  email    = "alice@example.com"
  password = "change-me-1!"
}

resource "mamori_role_grant" "alice_analysts" {
  role    = mamori_role.analysts.name
  grantee = mamori_user.alice.username
}

resource "mamori_permission" "analysts_websql" {
  grantee    = mamori_role.analysts.name
  type       = "mamori"
  privileges = ["WEB SQL EDITOR"]
}
```

[`examples/main.tf`](examples/main.tf) has a fuller configuration covering every resource.

### Provider configuration

| Attribute              | Environment variable          | Description                                          |
| ---------------------- | ----------------------------- | ---------------------------------------------------- |
| `server`               | `MAMORI_SERVER`               | Base URL of the mamori server.                       |
| `username`             | `MAMORI_USERNAME`             | User to log in as.                                   |
| `password`             | `MAMORI_PASSWORD`             | Password of the login user (sensitive).              |
| `insecure_skip_verify` | `MAMORI_INSECURE_SKIP_VERIFY` | Skip TLS certificate verification. Defaults to false. |

Values in the provider block take precedence over the environment. `server`, `username` and `password` are required one way or the other. The provider logs in once per run.

## Resources

| Resource                      | Manages                                                     | Import ID      |
| ----------------------------- | ----------------------------------------------------------- | -------------- |
| `mamori_user`                 | A password-authenticated user                               | `username`     |
| `mamori_role`                 | A role                                                      | `name`         |
| `mamori_role_grant`           | A role granted to a user or role                            | `role:grantee` |
| `mamori_permission`           | A permission granted to a user or role                      | not importable |
| `mamori_datasource`           | A database proxied by mamori                                | `name`         |
| `mamori_secret`               | A secret in the mamori vault (single value or multi-part)   | `name`         |
| `mamori_ip_resource`          | A named network range                                       | `name`         |
| `mamori_http_resource`        | A proxied web application                                   | `name`         |
| `mamori_ssh_login`            | A stored SSH target and credentials                         | `name`         |
| `mamori_remote_desktop_login` | A stored RDP or VNC login                                   | `name`         |

Objects are identified by name rather than server id, so renaming one replaces it.

```sh
tofu import mamori_role.analysts analysts
tofu import mamori_role_grant.alice_analysts analysts:alice
```

### Permissions

`mamori_permission` grants one permission, chosen by `type`:

| `type`                                                                                                      | Attributes used                                                                                    |
| ----------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `datasource`                                                                                                | `privileges`, `datasource`, `database`, `schema`, `object`, optionally `where_clause`, `row_limit` |
| `mamori`                                                                                                    | `privileges` (server privileges such as `CREATE USER`)                                             |
| `credential`                                                                                                | `datasource`                                                                                       |
| `ip_resource`                                                                                               | `name`, optionally `unauthenticated`                                                               |
| `policy`, `encryption_key`, `ssh`, `sftp`, `remote_desktop`, `http_resource`, `secret`, `script`, `script_flow` | `name`                                                                                             |

All types also accept `with_grant_option`, `valid_from` and `valid_until` (`"YYYY-MM-DD HH:mm"`). Every attribute forces replacement: changing a permission revokes the old grant and grants the new one. Roles are granted with `mamori_role_grant`, not `mamori_permission`.

```hcl
resource "mamori_permission" "analysts_select" {
  grantee    = mamori_role.analysts.name
  type       = "datasource"
  privileges = ["SELECT"]
  datasource = mamori_datasource.pg.name
  database   = "*"
  schema     = "*"
  object     = "*"
}
```

## Limitations

These come from what the mamori server API exposes:

- Secret values and SSH, remote desktop and datasource passwords are never returned by the server, so changes made outside Terraform are not detected.
- `mamori_user.password` is only used when the user is created. Changing it later has no effect.
- `mamori_datasource` only detects whether the datasource exists. Removing a setting from the configuration does not clear it on the server.
- Policy permissions can't be read back, so a `policy` grant is assumed to still exist.

## Development

Requires Go 1.27.1.

```sh
go build ./...
go vet ./...
go test ./...
```

The tests are unit tests and don't need a server.

### Installing a local build

To use a local build from any configuration through the normal `init` flow, run:

```sh
scripts/install-local.sh          # installs as version 0.0.1
scripts/install-local.sh 0.0.2    # or any other version
```

The script builds the provider and copies it into the local plugin directory: `%APPDATA%\terraform.d\plugins` on Windows (run it from Git Bash) and `~/.terraform.d/plugins` elsewhere. Terraform and OpenTofu then install it from there instead of the registry, as long as no CLI config defines a `provider_installation` block. After installing a rebuild under the same version, delete `.terraform.lock.hcl` in your configuration and run `init` again, because the lock file rejects the new checksum.

### Trying a build without installing

Alternatively, build the provider into a directory:

```sh
go build -o ./bin/ .
```

Create a CLI config file, e.g. `dev.tfrc`, that points at it:

```hcl
provider_installation {
  dev_overrides {
    "mamori-io/mamori" = "/absolute/path/to/bin"
  }
  direct {}
}
```

Then, in a directory with a configuration such as `examples/`, skip `init` and run:

```sh
TF_CLI_CONFIG_FILE=/absolute/path/to/dev.tfrc tofu validate
TF_CLI_CONFIG_FILE=/absolute/path/to/dev.tfrc tofu plan
```

`plan` and `apply` need a reachable mamori server and credentials. The same steps work with `terraform` in place of `tofu`.

### Releasing

Pushing a `v*` tag runs the [release workflow](.github/workflows/release.yml), which tests the provider and uses [GoReleaser](https://goreleaser.com) to publish a GitHub release in the layout the Terraform and OpenTofu registries expect:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The checksum file is signed with the GPG key in the `GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets. The registries verify it against the public key registered for the namespace. To check the GoReleaser setup locally without signing or publishing:

```sh
go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=sign,publish
```

### Client dependency

The client is imported as `mamori.io/mamori-go-client` and redirected to `github.com/mamori-io/mamori-go-client` by a `replace` directive in `go.mod`. The `replace` pins the version, so to update the client, change the version on the `replace` line (keep the line itself) and run `go mod tidy`:

```sh
go mod edit -replace mamori.io/mamori-go-client=github.com/mamori-io/mamori-go-client@main
go mod tidy
```
