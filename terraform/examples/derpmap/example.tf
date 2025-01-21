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

resource "tacl_derpmap" "myderp" {
  derpmap_json = jsonencode({
    regions = {
      1 = {
        regionID   = 1
        regionName = "some-region"
      },
      2 = {
        regionID   = 2
        regionName = "another-region"
      }
    }
  })
}

data "tacl_derpmap" "check" {}