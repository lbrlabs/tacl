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


resource "tacl_nodeattr" "example_attr" {
  target = ["*"]
  attr   = ["nextdns:abc123", "nextdns:no-device-info"]
}

resource "tacl_nodeattr" "google" {
  target = ["*"]

  app_json = jsonencode({
    "tailscale.com/app-connectors" = [
      {
        "name"       = "google",
        "connectors" = ["tag:router"],
        "domains"    = ["google.com"]
      }
    ]
  })
}
