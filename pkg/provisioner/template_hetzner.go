package provisioner

func init() {
	RegisterTemplate("hetzner", hetznerCloudTemplate)
}

const hetznerCloudTemplate = `
terraform {
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = ">= 1.45.0"
    }
  }
}

provider "hcloud" {
  token = var.api_token
}

variable "api_token" {
  description = "Hetzner Cloud API token"
  type        = string
  sensitive   = true
  default     = "{{ .APIToken }}"
}

{{- if .SSHKeyID }}

data "hcloud_ssh_key" "default" {
  name = "{{ .SSHKeyID }}"
}
{{- end }}

# --- Import existing nodes ---
{{- range $_, $existing := .ExistingNodes }}

import {
  to = hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_{{ $existing.Index }}
  id = "{{ $existing.ImportID }}"
}
{{- end }}

{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}

resource "hcloud_server" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}" {
  name        = "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}"
  server_type = "{{ $pool.Machine }}"
  image       = "{{ $.Image }}"
  location    = "{{ $.Zone }}"
{{- if $.SSHKeyID }}
  ssh_keys    = [data.hcloud_ssh_key.default.id]
{{- end }}

  labels = {
    env        = "{{ $.EnvName }}"
    pool       = "{{ $pool.Name }}"
    managed-by = "plasmactl"
  }
}
{{- end }}
{{- end }}

output "servers" {
  description = "Provisioned servers"
  value = {
{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}
    "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}" = {
      hostname    = hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.name
      public_ip   = hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.ipv4_address
      failover_ip = ""
      private_ip  = ""
      private_mac = ""
      server_id   = tostring(hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.id)
      zone        = hcloud_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.location
      region      = ""
      machine     = "{{ $pool.Machine }}"
      pool        = "{{ $pool.Name }}"
      provider    = "hetzner"
    }
{{- end }}
{{- end }}
  }
}
`
