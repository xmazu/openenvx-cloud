resource "cloudflare_worker_script" "this" {
  account_id = var.account_id
  name       = var.name
  content    = var.script_content
  module     = true
}

resource "cloudflare_worker_domain" "this" {
  count      = var.hostname != "" ? 1 : 0
  account_id = var.account_id
  hostname   = var.hostname
  service    = cloudflare_worker_script.this.name
  zone_id    = var.zone_id
}