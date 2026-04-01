output "worker_name" {
  description = "The deployed Cloudflare Worker script name"
  value       = cloudflare_worker_script.this.name
}

output "worker_domain" {
  description = "The configured worker domain"
  value       = var.hostname != "" ? cloudflare_worker_domain.this[0].hostname : ""
}