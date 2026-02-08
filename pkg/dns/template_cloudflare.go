package dns

func init() {
	RegisterTemplate("cloudflare", cloudflareDNSTemplate)
}

const cloudflareDNSTemplate = `
terraform {
  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = ">= 4.0.0"
    }
  }
}

provider "cloudflare" {
  api_token = var.api_token
}

variable "api_token" {
  description = "Cloudflare API token"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

data "cloudflare_zone" "zone" {
  name = "{{ .Zone }}"
}

# --- A records (web ingress) ---
{{- if .WebIP }}

resource "cloudflare_record" "a" {
  zone_id = data.cloudflare_zone.zone.id
{{- if .Subdomain }}
  name    = "{{ .Subdomain }}"
{{- else }}
  name    = "@"
{{- end }}
  type    = "A"
  content = "{{ .WebIP }}"
  ttl     = 3600
  proxied = false
}

resource "cloudflare_record" "wildcard_a" {
  zone_id = data.cloudflare_zone.zone.id
{{- if .Subdomain }}
  name    = "*.{{ .Subdomain }}"
{{- else }}
  name    = "*"
{{- end }}
  type    = "A"
  content = "{{ .WebIP }}"
  ttl     = 3600
  proxied = false
}
{{- end }}

# --- Mail records ---
{{- if .MailIP }}

resource "cloudflare_record" "mail_a" {
  zone_id = data.cloudflare_zone.zone.id
{{- if .Subdomain }}
  name    = "mail.{{ .Subdomain }}"
{{- else }}
  name    = "mail"
{{- end }}
  type    = "A"
  content = "{{ .MailIP }}"
  ttl     = 3600
  proxied = false
}

resource "cloudflare_record" "mx" {
  zone_id  = data.cloudflare_zone.zone.id
{{- if .Subdomain }}
  name     = "{{ .Subdomain }}"
{{- else }}
  name     = "@"
{{- end }}
  type     = "MX"
  content  = "mail.{{ .Domain }}."
  priority = 10
  ttl      = 3600
}

resource "cloudflare_record" "spf" {
  zone_id = data.cloudflare_zone.zone.id
{{- if .Subdomain }}
  name    = "{{ .Subdomain }}"
{{- else }}
  name    = "@"
{{- end }}
  type    = "TXT"
  content = "v=spf1 ip4:{{ .MailIP }} -all"
  ttl     = 3600
}
{{- end }}

# --- DMARC ---

resource "cloudflare_record" "dmarc" {
  zone_id = data.cloudflare_zone.zone.id
{{- if .Subdomain }}
  name    = "_dmarc.{{ .Subdomain }}"
{{- else }}
  name    = "_dmarc"
{{- end }}
  type    = "TXT"
  content = "v=DMARC1; p=quarantine; rua=mailto:dmarc@{{ .Domain }}"
  ttl     = 3600
}

# --- DKIM ---
{{- if .DKIMKey }}

resource "cloudflare_record" "dkim" {
  zone_id = data.cloudflare_zone.zone.id
{{- if .Subdomain }}
  name    = "{{ .DKIMSelector }}._domainkey.{{ .Subdomain }}"
{{- else }}
  name    = "{{ .DKIMSelector }}._domainkey"
{{- end }}
  type    = "TXT"
  content = "v=DKIM1; k=rsa; p={{ .DKIMKey }}"
  ttl     = 3600
}
{{- end }}
`
