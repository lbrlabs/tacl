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

resource "tacl_auto_approvers" "main" {
  routes = {
    "0.0.0.0/0" = ["tag:router"]
  }
  exit_node = ["tag:router"]
}

data "tacl_auto_approvers" "check" {}
  
