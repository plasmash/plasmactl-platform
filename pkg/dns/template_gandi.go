package dns

func init() {
	RegisterTemplate("gandi", gandiDNSTemplate)
}

const gandiDNSTemplate = `
terraform {
  required_providers {
    gandi = {
      source  = "go-gandi/gandi"
      version = ">= 2.3.0"
    }
  }
}

provider "gandi" {
  personal_access_token = var.api_token
}

variable "api_token" {
  description = "Gandi Personal Access Token"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

# --- A records (web ingress) ---
{{- if .WebIP }}

resource "gandi_livedns_record" "a" {
  zone   = "{{ .Zone }}"
{{- if .Subdomain }}
  name   = "{{ .Subdomain }}"
{{- else }}
  name   = "@"
{{- end }}
  type   = "A"
  ttl    = 3600
  values = ["{{ .WebIP }}"]
}

resource "gandi_livedns_record" "wildcard_a" {
  zone   = "{{ .Zone }}"
{{- if .Subdomain }}
  name   = "*.{{ .Subdomain }}"
{{- else }}
  name   = "*"
{{- end }}
  type   = "A"
  ttl    = 3600
  values = ["{{ .WebIP }}"]
}
{{- end }}

# --- Mail records ---
{{- if .MailIP }}

resource "gandi_livedns_record" "mail_a" {
  zone   = "{{ .Zone }}"
{{- if .Subdomain }}
  name   = "mail.{{ .Subdomain }}"
{{- else }}
  name   = "mail"
{{- end }}
  type   = "A"
  ttl    = 3600
  values = ["{{ .MailIP }}"]
}

resource "gandi_livedns_record" "mx" {
  zone   = "{{ .Zone }}"
{{- if .Subdomain }}
  name   = "{{ .Subdomain }}"
{{- else }}
  name   = "@"
{{- end }}
  type   = "MX"
  ttl    = 3600
  values = ["10 mail.{{ .Domain }}."]
}

resource "gandi_livedns_record" "spf" {
  zone   = "{{ .Zone }}"
{{- if .Subdomain }}
  name   = "{{ .Subdomain }}"
{{- else }}
  name   = "@"
{{- end }}
  type   = "TXT"
  ttl    = 3600
  values = ["\"v=spf1 ip4:{{ .MailIP }} -all\""]
}
{{- end }}

# --- DMARC ---

resource "gandi_livedns_record" "dmarc" {
  zone   = "{{ .Zone }}"
{{- if .Subdomain }}
  name   = "_dmarc.{{ .Subdomain }}"
{{- else }}
  name   = "_dmarc"
{{- end }}
  type   = "TXT"
  ttl    = 3600
  values = ["\"v=DMARC1; p=quarantine; rua=mailto:dmarc@{{ .Domain }}\""]
}

# --- DKIM ---
{{- if .DKIMKey }}

resource "gandi_livedns_record" "dkim" {
  zone   = "{{ .Zone }}"
{{- if .Subdomain }}
  name   = "{{ .DKIMSelector }}._domainkey.{{ .Subdomain }}"
{{- else }}
  name   = "{{ .DKIMSelector }}._domainkey"
{{- end }}
  type   = "TXT"
  ttl    = 3600
  values = ["\"v=DKIM1; k=rsa; p={{ .DKIMKey }}\""]
}
{{- end }}
`
