package provisioner

func init() {
	RegisterTemplate("scaleway", scalewayDediboxTemplate)
}

const scalewayDediboxTemplate = `
terraform {
  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = ">= 2.50.0"
    }
  }
}

provider "scaleway" {
  secret_key = var.api_token
  zone       = "{{ .Zone }}"
}

variable "api_token" {
  description = "Scaleway API secret key"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

{{- if .ProjectID }}

variable "project_id" {
  description = "Scaleway project ID"
  type        = string
  default     = "{{ .ProjectID }}"
}
{{- end }}

# --- Import existing nodes ---
{{- range $_, $existing := .ExistingNodes }}

import {
  to = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_{{ $existing.Index }}
  id = "{{ $existing.ImportID }}"
}
{{- end }}

# --- Offer lookups ---
{{- range $i, $pool := .Pools }}

data "scaleway_dedibox_offer" "offer_{{ $i }}" {
  name = "{{ $pool.Machine }}"
}
{{- end }}

# --- Server ordering ---
{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}

resource "scaleway_dedibox_server" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}" {
  offer_id = data.scaleway_dedibox_offer.offer_{{ $i }}.offer_id
{{- if $.ProjectID }}
  project_id = var.project_id
{{- end }}
}
{{- end }}
{{- end }}

# --- OS installation ---
{{- if .Image }}
{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}

resource "scaleway_dedibox_server_install" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}" {
  server_id = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.id
  os_id     = "{{ $.Image }}"
  hostname  = "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}"
{{- if $.SSHKeyID }}
  ssh_key_ids = ["{{ $.SSHKeyID }}"]
{{- end }}
}
{{- end }}
{{- end }}
{{- end }}

# --- Failover IPs ---
{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}

resource "scaleway_dedibox_failover_ip" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}" {
  offer_id  = 1
{{- if $.ProjectID }}
  project_id = var.project_id
{{- end }}
  server_id = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.id
}
{{- end }}
{{- end }}

# --- RPN group (private network) ---

resource "scaleway_dedibox_rpn_group" "{{ .EnvName | replace "-" "_" }}" {
  name = "{{ .EnvName }}"
{{- if .ProjectID }}
  project_id = var.project_id
{{- end }}
  type = "standard"
  server_ids = [
{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}
    scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.id,
{{- end }}
{{- end }}
  ]
}

output "servers" {
  description = "Provisioned servers"
  value = {
{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}
    "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}" = {
{{- if $.Image }}
      hostname    = scaleway_dedibox_server_install.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.hostname
{{- else }}
      hostname    = "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}"
{{- end }}
      public_ip   = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.ip[0].address
      failover_ip = scaleway_dedibox_failover_ip.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.address
      private_ip  = ""
      private_mac = ""
      server_id   = scaleway_dedibox_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.id
      zone        = "{{ $.Zone }}"
      region      = ""
      machine     = "{{ $pool.Machine }}"
      pool        = "{{ $pool.Name }}"
      provider    = "scaleway"
    }
{{- end }}
{{- end }}
  }
}
`
