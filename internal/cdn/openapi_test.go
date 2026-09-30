package cdn

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"
)

type contractSchema struct {
	Ref        string                    `yaml:"$ref"`
	Type       string                    `yaml:"type"`
	Const      any                       `yaml:"const"`
	Required   []string                  `yaml:"required"`
	Properties map[string]contractSchema `yaml:"properties"`
	Items      *contractSchema           `yaml:"items"`
}

type contractMedia struct {
	Schema contractSchema `yaml:"schema"`
}

type contractResponse struct {
	Content map[string]contractMedia `yaml:"content"`
}

type contractOperation struct {
	Responses map[string]contractResponse `yaml:"responses"`
}

type contractPath struct {
	Get contractOperation `yaml:"get"`
}

func TestOpenAPIContractHasTypedJSONResponses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OpenAPI    string                  `yaml:"openapi"`
		Paths      map[string]contractPath `yaml:"paths"`
		Components struct {
			Responses map[string]contractResponse `yaml:"responses"`
			Schemas   map[string]contractSchema   `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse OpenAPI document: %v", err)
	}
	if document.OpenAPI != "3.2.1" {
		t.Fatalf("OpenAPI version = %q, want 3.2.1", document.OpenAPI)
	}
	assertJSONResponse := func(path, code, media string) contractSchema {
		t.Helper()
		response, ok := document.Paths[path].Get.Responses[code]
		if !ok {
			t.Fatalf("missing %s response for %s", code, path)
		}
		body, ok := response.Content[media]
		if !ok {
			t.Fatalf("missing %s content for %s %s", media, path, code)
		}
		return body.Schema
	}
	for _, test := range []struct {
		path, value string
	}{
		{path: "/health", value: "ok"},
		{path: "/ready", value: "ready"},
	} {
		schema := assertJSONResponse(test.path, "200", "application/json")
		if schema.Type != "object" || schema.Properties["status"].Const != test.value {
			t.Errorf("%s response schema = %+v, want object status=%q", test.path, schema, test.value)
		}
	}
	icons := assertJSONResponse("/icons", "200", "application/json")
	if icons.Type != "object" || icons.Properties["lucide_version"].Type != "string" || icons.Properties["icons"].Type != "array" || icons.Properties["icons"].Items == nil || icons.Properties["icons"].Items.Type != "string" {
		t.Fatalf("icons response is not a typed JSON schema: %+v", icons)
	}
	problemResponse, ok := document.Components.Responses["Problem"]
	if !ok || problemResponse.Content["application/problem+json"].Schema.Ref != "#/components/schemas/Problem" {
		t.Fatal("problem response must reference the typed Problem schema")
	}
	problem, ok := document.Components.Schemas["Problem"]
	if !ok || problem.Type != "object" {
		t.Fatal("missing Problem object schema")
	}
	for _, required := range []string{"type", "title", "status", "code"} {
		if !containsString(problem.Required, required) {
			t.Errorf("Problem schema does not require %q", required)
		}
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
