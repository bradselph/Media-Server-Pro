// Package apispec embeds the OpenAPI specification for serving via /api/docs.
package apispec

import _ "embed"

// Spec is the raw OpenAPI YAML document embedded at build time.
//
//go:embed openapi.yaml
var Spec []byte
