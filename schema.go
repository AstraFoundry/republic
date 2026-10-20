package republic

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type localSchemaLoader struct{}

func (localSchemaLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is not registered: %s", url)
}
func validateOutput(schema map[string]any, value any) error {
	// Normalize typed Go slices/maps into JSON values accepted by the compiler.
	b, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(localSchemaLoader{})
	if err := compiler.AddResource("urn:republic:output", doc); err != nil {
		return err
	}
	compiled, err := compiler.Compile("urn:republic:output")
	if err != nil {
		return err
	}
	return compiled.Validate(value)
}

func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return cloneMap(x)
	case []any:
		r := make([]any, len(x))
		for i, v := range x {
			r[i] = cloneValue(v)
		}
		return r
	default:
		return v
	}
}
func cloneMap(v map[string]any) map[string]any {
	r := map[string]any{}
	for k, x := range v {
		r[k] = cloneValue(x)
	}
	return r
}
func DeepMerge(base, override map[string]any) map[string]any {
	r := cloneMap(base)
	for k, v := range override {
		a, aok := r[k].(map[string]any)
		b, bok := v.(map[string]any)
		if aok && bok {
			r[k] = DeepMerge(a, b)
		} else {
			r[k] = cloneValue(v)
		}
	}
	return r
}

// StrictSchema adapts JSON Schema to provider strict structured output rules.
func StrictSchema(schema map[string]any, requireAll bool) (map[string]any, error) {
	root := cloneMap(schema)
	var visit func(map[string]any, map[string]bool) (map[string]any, error)
	visit = func(n map[string]any, refs map[string]bool) (map[string]any, error) {
		for _, key := range []string{"$defs", "definitions", "properties"} {
			if children, ok := n[key].(map[string]any); ok {
				for k, v := range children {
					if m, ok := v.(map[string]any); ok {
						child, err := visit(m, refs)
						if err != nil {
							return nil, err
						}
						children[k] = child
					}
				}
			}
		}
		for _, key := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
			if children, ok := n[key].([]any); ok {
				for i, v := range children {
					if m, ok := v.(map[string]any); ok {
						child, err := visit(m, refs)
						if err != nil {
							return nil, err
						}
						children[i] = child
					}
				}
			}
		}
		for _, key := range []string{"items", "additionalProperties", "not"} {
			if m, ok := n[key].(map[string]any); ok {
				child, err := visit(m, refs)
				if err != nil {
					return nil, err
				}
				n[key] = child
			}
		}
		if n["type"] == "object" {
			if _, ok := n["additionalProperties"]; !ok {
				n["additionalProperties"] = false
			}
		}
		if !requireAll {
			return n, nil
		}
		if props, ok := n["properties"].(map[string]any); ok {
			keys := make([]string, 0, len(props))
			for k := range props {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			n["required"] = keys
		}
		if n["default"] == nil {
			delete(n, "default")
		}
		if all, ok := n["allOf"].([]any); ok && len(all) == 1 {
			if m, ok := all[0].(map[string]any); ok {
				delete(n, "allOf")
				for k, v := range m {
					n[k] = v
				}
			}
		}
		if ref, ok := n["$ref"].(string); ok && len(n) > 1 {
			if refs[ref] {
				return nil, fmt.Errorf("cyclic schema reference with siblings: %s", ref)
			}
			if !strings.HasPrefix(ref, "#/") {
				return nil, fmt.Errorf("unsupported schema reference: %s", ref)
			}
			var target any = root
			for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
				m, ok := target.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("invalid schema reference: %s", ref)
				}
				target = m[strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")]
			}
			m, ok := target.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid schema reference: %s", ref)
			}
			next := map[string]bool{}
			for k, v := range refs {
				next[k] = v
			}
			next[ref] = true
			inlined, err := visit(cloneMap(m), next)
			if err != nil {
				return nil, err
			}
			delete(n, "$ref")
			for k, v := range n {
				inlined[k] = v
			}
			n = inlined
		}
		return n, nil
	}
	return visit(cloneMap(root), map[string]bool{})
}
