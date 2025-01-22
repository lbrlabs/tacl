package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/lbrlabs/tacl/terraform/provider"
)

var (
	Version = "dev"
)

func main() {

	version := flag.Bool("version", false, "Print version and exit")

	flag.Parse()

	if *version {
		log.Printf("terraform-provider-tacl %s", Version)
		return
	}

	err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/lbrlabs/tacl",
	})
	if err != nil {
		log.Fatal(err)
	}
}
