package dns

func init() {
	RegisterTemplate("inwx", inwxDNSTemplate)
}

const inwxDNSTemplate = `
terraform {
  required_providers {
    inwx = {
      source  = "inwx/inwx"
      version = ">= 1.3.0"
    }
  }
}

provider "inwx" {
  api_url  = "https://api.domrobot.com/jsonrpc/"
  username = var.api_username
  password = var.api_token
}

variable "api_username" {
  description = "INWX account username"
  type        = string
  default     = "{{ .APIUsername }}"
}

variable "api_token" {
  description = "INWX account password"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

# --- A records (web ingress) ---
{{- if .WebIP }}

resource "inwx_domain_record" "a" {
  domain  = "{{ .Zone }}"
{{- if .Subdomain }}
  name    = "{{ .Subdomain }}.{{ .Zone }}"
{{- else }}
  name    = "{{ .Zone }}"
{{- end }}
  type    = "A"
  ttl     = 3600
  content = "{{ .WebIP }}"
}

resource "inwx_domain_record" "wildcard_a" {
  domain  = "{{ .Zone }}"
{{- if .Subdomain }}
  name    = "*.{{ .Subdomain }}.{{ .Zone }}"
{{- else }}
  name    = "*.{{ .Zone }}"
{{- end }}
  type    = "A"
  ttl     = 3600
  content = "{{ .WebIP }}"
}
{{- end }}

# --- Mail records ---
{{- if .MailIP }}

resource "inwx_domain_record" "mail_a" {
  domain  = "{{ .Zone }}"
{{- if .Subdomain }}
  name    = "mail.{{ .Subdomain }}.{{ .Zone }}"
{{- else }}
  name    = "mail.{{ .Zone }}"
{{- end }}
  type    = "A"
  ttl     = 3600
  content = "{{ .MailIP }}"
}

resource "inwx_domain_record" "mx" {
  domain  = "{{ .Zone }}"
{{- if .Subdomain }}
  name    = "{{ .Subdomain }}.{{ .Zone }}"
{{- else }}
  name    = "{{ .Zone }}"
{{- end }}
  type    = "MX"
  ttl     = 3600
  prio    = 10
  content = "mail.{{ .Domain }}."
}

resource "inwx_domain_record" "spf" {
  domain  = "{{ .Zone }}"
{{- if .Subdomain }}
  name    = "{{ .Subdomain }}.{{ .Zone }}"
{{- else }}
  name    = "{{ .Zone }}"
{{- end }}
  type    = "TXT"
  ttl     = 3600
  content = "\"v=spf1 ip4:{{ .MailIP }} -all\""
}
{{- end }}

# --- DMARC ---

resource "inwx_domain_record" "dmarc" {
  domain  = "{{ .Zone }}"
{{- if .Subdomain }}
  name    = "_dmarc.{{ .Subdomain }}.{{ .Zone }}"
{{- else }}
  name    = "_dmarc.{{ .Zone }}"
{{- end }}
  type    = "TXT"
  ttl     = 3600
  content = "\"v=DMARC1; p=quarantine; rua=mailto:dmarc@{{ .Domain }}\""
}

# --- DKIM ---
{{- if .DKIMKey }}

resource "inwx_domain_record" "dkim" {
  domain  = "{{ .Zone }}"
{{- if .Subdomain }}
  name    = "{{ .DKIMSelector }}._domainkey.{{ .Subdomain }}.{{ .Zone }}"
{{- else }}
  name    = "{{ .DKIMSelector }}._domainkey.{{ .Zone }}"
{{- end }}
  type    = "TXT"
  ttl     = 3600
  content = "\"v=DKIM1; k=rsa; p={{ .DKIMKey }}\""
}
{{- end }}
`
