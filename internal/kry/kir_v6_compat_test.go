package kry

func stripKIRV6WireFields(value any) {
	switch node := value.(type) {
	case map[string]any:
		for _, key := range []string{"id", "span", "import_records", "variant_spans"} {
			delete(node, key)
		}
		removeCoordinates := func() {
			delete(node, "source")
			delete(node, "line")
			delete(node, "column")
		}
		if _, hasFields := node["fields"]; hasFields && hasTypeParams(node) {
			removeCoordinates()
		} else if _, hasVariants := node["variants"]; hasVariants {
			removeCoordinates()
		} else if _, hasPublic := node["public"]; hasPublic {
			if _, hasType := node["type"]; hasType {
				removeCoordinates()
			} else if _, hasMethods := node["methods"]; hasMethods {
				removeCoordinates()
			}
		} else if _, hasConstraint := node["constraint"]; hasConstraint {
			removeCoordinates()
		} else if _, hasDefault := node["default"]; hasDefault {
			removeCoordinates()
		} else if _, hasPattern := node["pattern"]; hasPattern {
			removeCoordinates()
		} else if _, hasFor := node["for"]; hasFor {
			removeCoordinates()
		} else if _, hasTarget := node["target"]; hasTarget {
			if _, hasName := node["name"]; hasName && len(node) <= 5 {
				removeCoordinates()
			}
		} else if _, hasReturn := node["return"]; hasReturn {
			if _, hasBody := node["body"]; !hasBody {
				removeCoordinates()
			}
		}
		for _, child := range node {
			stripKIRV6WireFields(child)
		}
	case []any:
		for _, child := range node {
			stripKIRV6WireFields(child)
		}
	}
}

func hasTypeParams(node map[string]any) bool {
	_, ok := node["type_params"]
	return ok
}
