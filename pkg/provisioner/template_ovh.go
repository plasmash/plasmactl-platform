package provisioner

func init() {
	RegisterTemplate("ovh", ovhDedicatedTemplate)
}

const ovhDedicatedTemplate = `
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

{{- if .ProjectID }}

variable "vrack_service_name" {
  description = "OVH vRack service name for private networking"
  type        = string
  default     = "{{ .ProjectID }}"
}
{{- end }}

data "ovh_me" "account" {}

# --- Import existing nodes ---
{{- range $_, $existing := .ExistingNodes }}

import {
  to = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_{{ $existing.Index }}
  id = "{{ $existing.ImportID }}"
}
{{- end }}

{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}

resource "ovh_dedicated_server" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}" {
  ovh_subsidiary = data.ovh_me.account.ovh_subsidiary
  display_name   = "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}"
  os             = "{{ $.Image }}"

  plan = [{
    plan_code    = "{{ $pool.Machine }}"
    duration     = "P1M"
    pricing_mode = "default"

    configuration = [
      {
        label = "dedicated_datacenter"
        value = "{{ $.Zone }}"
      },
      {
        label = "dedicated_os"
        value = "none_64.en"
      },
      {
        label = "region"
        value = "{{ $.Region }}"
      }
    ]
  }]

{{- if $.SSHKeyID }}

  customizations = {
    hostname = "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}"
    ssh_key  = var.ssh_key
  }
{{- else }}

  customizations = {
    hostname = "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}"
  }
{{- end }}
}

{{- if $.ProjectID }}

# Attach server to vRack for private networking
data "ovh_dedicated_server" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}_data" {
  service_name = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.name
}

resource "ovh_vrack_dedicated_server_interface" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}_vrack" {
  service_name = var.vrack_service_name
  interface_id = data.ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}_data.enabled_vrack_vnis[0]
}
{{- end }}
{{- end }}
{{- end }}

{{- if .SSHKeyID }}

variable "ssh_key" {
  description = "SSH public key for server access"
  type        = string
  default     = "{{ .SSHKeyID }}"
}
{{- end }}

output "servers" {
  description = "Provisioned servers"
  value = {
{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}
    "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}" = {
      hostname    = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.display_name
      public_ip   = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.ip
      failover_ip = ""
      private_ip  = ""
{{- if $.ProjectID }}
      private_mac = try(data.ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}_data.vnis[1].nics[0], "")
{{- else }}
      private_mac = ""
{{- end }}
      server_id   = tostring(ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.server_id)
      zone        = "{{ $.Zone }}"
      region      = "{{ $.Region }}"
      machine     = "{{ $pool.Machine }}"
      pool        = "{{ $pool.Name }}"
      provider    = "ovh"
    }
{{- end }}
{{- end }}
  }
}
`
