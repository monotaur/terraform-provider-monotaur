//go:build tools

// Package tools tracks tool-only dependencies so they appear in go.mod/go.sum.
//
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name monotaur
// To regenerate docs run: make docs
package tools

import (
	// tfplugindocs generates Registry-compatible Markdown docs from provider
	// schema descriptions and the examples/ tree.
	_ "github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs"
)
