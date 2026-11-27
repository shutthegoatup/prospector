package api

import (
	_ "embed"
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"
)

//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml
//go:generate go tool oapi-codegen -config signer/oapi-codegen.yaml signer/openapi.yaml

// Source is openapi.yaml as written. The generated GetSpec renames operations to Go's
// taste; what is published keeps the spec's own names.
//
//go:embed openapi.yaml
var Source []byte

// Spec is the published spec, parsed and checked.
func Spec() (*openapi3.T, error) {
	spec, err := openapi3.NewLoader().LoadFromData(Source)
	if err != nil {
		return nil, err
	}
	return spec, spec.Validate(openapi3.NewLoader().Context)
}

// SpecJSON is the published spec as JSON.
func SpecJSON() ([]byte, error) {
	spec, err := Spec()
	if err != nil {
		return nil, err
	}
	return json.Marshal(spec)
}
