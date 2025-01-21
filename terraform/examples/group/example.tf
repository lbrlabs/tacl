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

resource "tacl_group" "example" {
  name        = "engineering"
  members     = ["mail@lbrlabs.com"]
  description = "Engineering team"
}

data "tacl_group" "engineering" {
  name = tacl_group.example.name
}

output "eng_members" {
  value = data.tacl_group.engineering.members
}

