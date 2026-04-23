package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/monotaur/terraform-provider-monotaur/internal/provider"
)

// version must remain a package-level variable in package main so that the
// build pipeline can inject the release tag at link time with:
//
//	-ldflags "-X main.version=v1.2.3"
//
// Moving it to any other package would break that injection.
var version = "dev"

func main() {
	var debug bool

	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/monotaur/monotaur",
		Debug:   debug,
	}

	err := providerserver.Serve(context.Background(), provider.New(version), opts)
	if err != nil {
		log.Fatal(err.Error())
	}
}
