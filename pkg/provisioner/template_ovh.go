package provisioner

func init() {
	RegisterTemplate("ovh", ovhDedicatedTemplate)
}

// ovhDedicatedTemplate renders Terraform HCL for OVH dedicated servers.
//
// Adoption-vs-creation contract (split-resource design — see
// docs/superpowers/specs/2026-04-29-node-join-ovh-adoption-design.md):
//
//   - Each ExistingNode emits a paired `import {}` + minimal resource block
//     under the address `<env>_<pool>_existing_<idx>`. The resource declares
//     only `service_name` + `prevent_install_on_import = true` +
//     `lifecycle { ignore_changes = all }`. The OVH provider's Read cannot
//     populate the order-block attributes (ovh_subsidiary / plan / etc.) from
//     the API, so declaring them on an imported resource would diff as
//     ForceNew → terminate. Adopted resources therefore permanently omit them.
//
//   - Each pool's fresh-create count is `pool.Count - countExisting(pool)`.
//     Fresh resources emit under `<env>_<pool>_<j>` and carry the full order
//     block (today's existing shape).
//
//   - Output `servers` is a single map keyed by ImportID (for adopted) or
//     auto-name `<env>-<pool>-<NNN>` (for fresh). The two key namespaces
//     never collide (canonical OVH names contain dots; auto-names don't).
//     The plasmactl-node action consumes outputs by matching `server_id`,
//     not by map key.
//
// Provider auth supports two credential shapes:
//   - new shape (plasmactl-auth): OVHClientID/ClientSecret/Endpoint set →
//     provider block uses client_id + client_secret + endpoint vars.
//   - legacy shape: APIToken set → provider uses access_token var.
const ovhDedicatedTemplate = `
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
  description = "OVH universe for the credential (ovh-eu / ovh-ca / ovh-us)"
  type        = string
  default     = "{{ .OVHEndpoint }}"
}

variable "ovh_client_id" {
  description = "OVH OAuth2 application key"
  type        = string
  sensitive   = true
  default     = "{{ .OVHClientID }}"
}

variable "ovh_client_secret" {
  description = "OVH OAuth2 application secret"
  type        = string
  sensitive   = true
  default     = "{{ .OVHClientSecret }}"
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

{{- if .ProjectID }}

variable "vrack_service_name" {
  description = "OVH vRack service name for private networking"
  type        = string
  default     = "{{ .ProjectID }}"
}
{{- end }}

data "ovh_me" "account" {}

# === Adopted resources (one per ExistingNode) ===
{{- range $_, $existing := .ExistingNodes }}

import {
  to = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_existing_{{ $existing.Index }}
  id = "{{ $existing.ImportID }}"
}

resource "ovh_dedicated_server" "{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_existing_{{ $existing.Index }}" {
  service_name              = "{{ $existing.ImportID }}"
  prevent_install_on_import = true
{{- if $existing.Hostname }}
  display_name              = "{{ $existing.Hostname }}"

  lifecycle {
    ignore_changes = [plan, ovh_subsidiary, os, customizations]
  }
{{- else }}

  lifecycle {
    ignore_changes = all
  }
{{- end }}
}
{{- end }}

# === Fresh-create resources (count = pool.Count - existingInPool) ===
{{- range $i, $pool := .Pools }}
{{- $freshCount := sub $pool.Count (countExisting $pool.Name $.ExistingNodes) }}
{{- if gt $freshCount 0 }}
{{- range $j := seq 0 $freshCount }}

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

  lifecycle {
    ignore_changes = [display_name]
  }
}

{{- if $.ProjectID }}

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
{{- end }}

{{- if .SSHKeyID }}

variable "ssh_key" {
  description = "SSH public key for server access"
  type        = string
  default     = "{{ .SSHKeyID }}"
}
{{- end }}

# === Outputs (merge adopted + fresh) ===
output "servers" {
  description = "Provisioned servers"
  value = merge(
    {
{{- range $_, $existing := .ExistingNodes }}
      "{{ $existing.ImportID }}" = {
        hostname    = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_existing_{{ $existing.Index }}.display_name
        public_ip   = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_existing_{{ $existing.Index }}.ip
        failover_ip = ""
        private_ip  = ""
        private_mac = ""
        server_id   = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_existing_{{ $existing.Index }}.name
        zone        = "{{ $.Zone }}"
        region      = "{{ $.Region }}"
        machine     = "{{ poolMachine $existing.Pool $.Pools }}"
        pool        = "{{ $existing.Pool }}"
        provider    = "ovh"
      }
{{- end }}
    },
    {
{{- range $i, $pool := .Pools }}
{{- $freshCount := sub $pool.Count (countExisting $pool.Name $.ExistingNodes) }}
{{- if gt $freshCount 0 }}
{{- range $j := seq 0 $freshCount }}
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
        server_id   = ovh_dedicated_server.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.name
        zone        = "{{ $.Zone }}"
        region      = "{{ $.Region }}"
        machine     = "{{ $pool.Machine }}"
        pool        = "{{ $pool.Name }}"
        provider    = "ovh"
      }
{{- end }}
{{- end }}
{{- end }}
    },
  )
}
`
