variable "api_token" {
  description = "The Cloudflare API token"
  type        = string
  sensitive   = true
}

variable "account_id" {
  description = "The Cloudflare account ID"
  type        = string
}

variable "zone_id" {
  description = "The Cloudflare zone ID for worker routing"
  type        = string
}

variable "name" {
  description = "The name of the Cloudflare Worker script"
  type        = string
}

variable "script_content" {
  description = "The content of the Worker script"
  type        = string
}

variable "hostname" {
  description = "The hostname to route the worker to"
  type        = string
  default     = ""
}