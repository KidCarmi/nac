// Package openapi embeds the OpenAPI 3.1 specification so the running service
// can serve its own contract at /openapi.yaml. Keeping the spec in the binary
// guarantees the documented contract always matches the deployed version.
package openapi

import _ "embed"

//go:embed openapi.yaml
var spec []byte

// Spec returns the raw OpenAPI document bytes.
func Spec() []byte { return spec }
