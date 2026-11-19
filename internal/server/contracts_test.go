package server

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/waste-not-app/waste-not-backend/contracts"
)

var schemaOnce sync.Once
var coreSchemas map[string]*jsonschema.Schema
var coreSchemaError error
var coreDocument map[string]any

func checkCoreResponse(t *testing.T, method, path string, status int, value any) {
	t.Helper()
	if status == 204 {
		return
	}
	schemaOnce.Do(func() {
		raw, e := contracts.Files.ReadFile("core.openapi.json")
		if e != nil {
			coreSchemaError = e
			return
		}
		if e = json.Unmarshal(raw, &coreDocument); e != nil {
			coreSchemaError = e
			return
		}
		c := jsonschema.NewCompiler()
		c.AssertFormat()
		if e = c.AddResource("urn:wastenot:core", coreDocument); e != nil {
			coreSchemaError = e
			return
		}
		coreSchemas = map[string]*jsonschema.Schema{}
		for name := range coreDocument["components"].(map[string]any)["schemas"].(map[string]any) {
			schema, e := c.Compile("urn:wastenot:core#/components/schemas/" + name)
			if e != nil {
				coreSchemaError = e
				return
			}
			coreSchemas[name] = schema
		}
	})
	if coreSchemaError != nil {
		t.Fatal(coreSchemaError)
	}
	path = strings.SplitN(path, "?", 2)[0]
	if strings.HasPrefix(path, "/v1/ai/") {
		return
	}
	if strings.HasPrefix(path, "/v1/waste-logs/") {
		path = "/v1/waste-logs/{id}"
	}
	if strings.HasPrefix(path, "/v1/waste-guide/") {
		path = "/v1/waste-guide/{id}"
	}
	if strings.HasPrefix(path, "/v1/photos/") {
		path = "/v1/photos/{id}"
	}
	var name string
	if status >= 400 {
		name = "Error"
	} else {
		paths := coreDocument["paths"].(map[string]any)
		p, ok := paths[path].(map[string]any)
		if !ok {
			return
		}
		op, ok := p[strings.ToLower(method)].(map[string]any)
		if !ok {
			return
		}
		responses := op["responses"].(map[string]any)
		key := strconv.Itoa(status)
		response := responses[key].(map[string]any)
		schema := response["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		if ref, ok := schema["$ref"].(string); ok {
			name = strings.TrimPrefix(ref, "#/components/schemas/")
		} else {
			return
		}
	}
	if e := coreSchemas[name].Validate(value); e != nil {
		t.Fatalf("%s %s %d violates %s: %v", method, path, status, name, e)
	}
}
