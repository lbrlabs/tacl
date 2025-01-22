## Terraform

Tacl also comes with an [experimental Terraform provider](https://github.com/lbrlabs/terraform-provider-tacl) that you can use to push resources to Tacl.

```hcl
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

resource "tacl_host" "example" {
  name = "example-host-1"
  ip   = "10.1.2.3"
}
```