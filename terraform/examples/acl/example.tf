terraform {
  required_providers {
    tacl = {
      source  = "lbrlabs/tacl"
      version = "~> 1.0"
    }
  }
}

provider "tacl" {
  endpoint = "http://tacl:8080"
}

resource "tacl_acl" "tacl_web_port" {
  action = "accept"
  src    = ["mail@lbrlabs.com"]
  proto  = "tcp"
  dst    = ["tag:tacl:8080",]
}

data "tacl_acl" "tacl_lookup" {
  # Reads the same entry from TACL by index:
  id = tacl_acl.tacl_web_port.id
}

output "tacl_proto" {
  value = data.tacl_acl.tacl_lookup.proto
}
