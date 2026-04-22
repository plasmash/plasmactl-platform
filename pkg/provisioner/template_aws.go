package provisioner

func init() {
	RegisterTemplate("aws", awsEC2Template)
}

const awsEC2Template = `
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0.0"
    }
  }
}

provider "aws" {
  region = "{{ .Region }}"
}

{{- if .SSHKeyID }}

data "aws_key_pair" "default" {
  key_name = "{{ .SSHKeyID }}"
}
{{- end }}

# --- Import existing nodes ---
{{- range $_, $existing := .ExistingNodes }}

import {
  to = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $existing.Pool | replace "-" "_" }}_{{ $existing.Index }}
  id = "{{ $existing.ImportID }}"
}
{{- end }}

{{- range $i, $pool := .Pools }}
{{- range $j := seq 0 $pool.Count }}

resource "aws_instance" "{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}" {
  ami               = "{{ $.Image }}"
  instance_type     = "{{ $pool.Machine }}"
  availability_zone = "{{ $.Zone }}"
{{- if $.SSHKeyID }}
  key_name          = data.aws_key_pair.default.key_name
{{- end }}

  tags = {
    Name       = "{{ $.EnvName }}-{{ $pool.Name }}-{{ printf "%03d" (add $j 1) }}"
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
      hostname    = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.tags["Name"]
      public_ip   = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.public_ip
      failover_ip = ""
      private_ip  = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.private_ip
      private_mac = ""
      server_id   = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.id
      zone        = aws_instance.{{ $.EnvName | replace "-" "_" }}_{{ $pool.Name | replace "-" "_" }}_{{ $j }}.availability_zone
      region      = "{{ $.Region }}"
      machine     = "{{ $pool.Machine }}"
      pool        = "{{ $pool.Name }}"
      provider    = "aws"
    }
{{- end }}
{{- end }}
  }
}
`
