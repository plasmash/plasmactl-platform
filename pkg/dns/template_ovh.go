package dns

func init() {
	RegisterTemplate("ovh", ovhDNSTemplate)
}

const ovhDNSTemplate = `
terraform {
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = ">= 2.11.0"
    }
  }
}

provider "ovh" {
  endpoint     = "ovh-{{ .Region }}"
  access_token = var.api_token
}

variable "api_token" {
  description = "OVH API access token"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

# --- A records (web ingress) ---
{{- if .WebIP }}

resource "ovh_domain_zone_record" "a" {
  zone      = "{{ .Zone }}"
{{- if .Subdomain }}
  subdomain = "{{ .Subdomain }}"
{{- end }}
  fieldtype = "A"
  ttl       = 3600
  target    = "{{ .WebIP }}"
}

resource "ovh_domain_zone_record" "wildcard_a" {
  zone      = "{{ .Zone }}"
{{- if .Subdomain }}
  subdomain = "*.{{ .Subdomain }}"
{{- else }}
  subdomain = "*"
{{- end }}
  fieldtype = "A"
  ttl       = 3600
  target    = "{{ .WebIP }}"
}
{{- end }}

# --- Mail records ---
{{- if .MailIP }}

resource "ovh_domain_zone_record" "mail_a" {
  zone      = "{{ .Zone }}"
{{- if .Subdomain }}
  subdomain = "mail.{{ .Subdomain }}"
{{- else }}
  subdomain = "mail"
{{- end }}
  fieldtype = "A"
  ttl       = 3600
  target    = "{{ .MailIP }}"
}

resource "ovh_domain_zone_record" "mx" {
  zone      = "{{ .Zone }}"
{{- if .Subdomain }}
  subdomain = "{{ .Subdomain }}"
{{- end }}
  fieldtype = "MX"
  ttl       = 3600
  target    = "10 mail.{{ .Domain }}."
}

resource "ovh_domain_zone_record" "spf" {
  zone      = "{{ .Zone }}"
{{- if .Subdomain }}
  subdomain = "{{ .Subdomain }}"
{{- end }}
  fieldtype = "TXT"
  ttl       = 3600
  target    = "\"v=spf1 ip4:{{ .MailIP }} -all\""
}
{{- end }}

# --- DMARC ---

resource "ovh_domain_zone_record" "dmarc" {
  zone      = "{{ .Zone }}"
{{- if .Subdomain }}
  subdomain = "_dmarc.{{ .Subdomain }}"
{{- else }}
  subdomain = "_dmarc"
{{- end }}
  fieldtype = "TXT"
  ttl       = 3600
  target    = "\"v=DMARC1; p=quarantine; rua=mailto:dmarc@{{ .Domain }}\""
}

# --- DKIM ---
{{- if .DKIMKey }}

resource "ovh_domain_zone_record" "dkim" {
  zone      = "{{ .Zone }}"
{{- if .Subdomain }}
  subdomain = "{{ .DKIMSelector }}._domainkey.{{ .Subdomain }}"
{{- else }}
  subdomain = "{{ .DKIMSelector }}._domainkey"
{{- end }}
  fieldtype = "TXT"
  ttl       = 3600
  target    = "\"v=DKIM1; k=rsa; p={{ .DKIMKey }}\""
}
{{- end }}
`
