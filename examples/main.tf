terraform {
  required_providers {
    mamori = {
      source = "mamori-io/mamori"
    }
  }
}

# server, username and password can instead come from MAMORI_SERVER,
# MAMORI_USERNAME and MAMORI_PASSWORD.
provider "mamori" {
  server               = "https://mamori.example.com"
  insecure_skip_verify = true
}

variable "pg_password" {
  type      = string
  sensitive = true
}

resource "mamori_role" "analysts" {
  name = "analysts"
}

resource "mamori_user" "alice" {
  username  = "alice"
  email     = "alice@example.com"
  full_name = "Alice Example"
  password  = "change-me-1!"
}

resource "mamori_role_grant" "alice_analysts" {
  role    = mamori_role.analysts.name
  grantee = mamori_user.alice.username
}

resource "mamori_datasource" "pg" {
  name     = "pg"
  type     = "POSTGRESQL"
  driver   = "postgres"
  host     = "10.0.2.2"
  port     = "5432"
  database = "mamori"
  user     = "postgres"
  password = var.pg_password
}

resource "mamori_permission" "analysts_select" {
  grantee    = mamori_role.analysts.name
  type       = "datasource"
  privileges = ["SELECT"]
  datasource = mamori_datasource.pg.name
  database   = "*"
  schema     = "*"
  object     = "*"
}

resource "mamori_permission" "analysts_websql" {
  grantee    = mamori_role.analysts.name
  type       = "mamori"
  privileges = ["WEB SQL EDITOR"]
}

resource "mamori_secret" "api_key" {
  name        = "reporting-api-key"
  secret      = "s3cr3t"
  description = "Key for the reporting API"
}

resource "mamori_permission" "analysts_api_key" {
  grantee = mamori_role.analysts.name
  type    = "secret"
  name    = mamori_secret.api_key.name
}

resource "mamori_ip_resource" "office" {
  name  = "office-lan"
  cidr  = "10.0.200.0/24"
  ports = "443,80"
}

resource "mamori_http_resource" "grafana" {
  name = "grafana"
  url  = "https://grafana.internal:3000"
}

resource "mamori_ssh_login" "bastion" {
  name     = "bastion"
  host     = "10.0.0.5"
  user     = "ops"
  password = "change-me"
}

resource "mamori_permission" "analysts_bastion" {
  grantee = mamori_role.analysts.name
  type    = "ssh"
  name    = mamori_ssh_login.bastion.name
}

# Analysts may request SSH/SFTP access to the bastion; the on-demand policy
# (created in mamori) decides who approves and for how long.
resource "mamori_requestable_resource" "analysts_bastion" {
  resource_type = "ssh_login"
  resource_name = mamori_ssh_login.bastion.name
  grantee       = mamori_role.analysts.name
  policy_name   = "ssh-access"
}

resource "mamori_remote_desktop_login" "jumpbox" {
  name     = "win-jumpbox"
  protocol = "rdp"
  host     = "10.0.0.20"
  username = "ops"
  password = "change-me"
  domain   = "CORP"
  security = "nla"
}

resource "mamori_permission" "analysts_jumpbox" {
  grantee = mamori_role.analysts.name
  type    = "remote_desktop"
  name    = mamori_remote_desktop_login.jumpbox.name
}
