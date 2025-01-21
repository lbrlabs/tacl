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

resource "tacl_host" "example" {
  name = "example-host-1"
  ip   = "10.1.2.3"
}

data "tacl_host" "lookup" {
  name = "example-host-1"
}

output "host_ip" {
  value = data.tacl_host.lookup.ip
}
