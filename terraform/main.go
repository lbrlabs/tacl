package main

import (
    "context"
    "log"

    "github.com/hashicorp/terraform-plugin-framework/providerserver"
    "github.com/lbrlabs/tacl/terraform/provider"
)

func main() {
    err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
        Address: "registry.terraform.io/lbrlabs/tacl",
    })
    if err != nil {
        log.Fatal(err)
    }
}
