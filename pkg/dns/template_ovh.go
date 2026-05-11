package dns

func init() {
	RegisterTemplate("ovh", ovhDNSTemplate)
}

// Template supports two credential shapes:
//   new shape (plasmactl-auth): OVHClientID/ClientSecret/Endpoint populated →
//     TF provider block uses client_id + client_secret + endpoint vars.
//   legacy shape: APIToken populated → TF provider uses access_token var with
//     endpoint constructed inline as "ovh-{{ .Region }}".
// The rest of the template (DNS records) is identical between the two shapes.
const ovhDNSTemplate = `
terraform {
  required_providers {
    ovh = {
      source  = "ovh/ovh"
      version = ">= 2.11.0"
    }
  }
}

{{- if .OVHClientID }}
provider "ovh" {
  endpoint      = var.ovh_endpoint
  client_id     = var.ovh_client_id
  client_secret = var.ovh_client_secret
}

variable "ovh_endpoint" {
  description = "OVH universe (ovh-eu / ovh-ca / ovh-us)"
  type        = string
  default     = "{{ .OVHEndpoint }}"
}

variable "ovh_client_id" {
  type      = string
  sensitive = true
  default   = "{{ .OVHClientID }}"
}

variable "ovh_client_secret" {
  type      = string
  sensitive = true
  default   = "{{ .OVHClientSecret }}"
}
{{- else }}
provider "ovh" {
  endpoint     = "ovh-{{ .Region }}"
  access_token = var.api_token
}

variable "api_token" {
  description = "OVH API access token (deprecated single-token form)"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}
{{- end }}

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
