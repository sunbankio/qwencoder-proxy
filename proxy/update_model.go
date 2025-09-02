package proxy

import "encoding/json"

func updateModel(requestBodyBytes []byte) []byte {
	var requestJSON map[string]interface{}
	err := json.Unmarshal(requestBodyBytes, &requestJSON)
	if err != nil {
		return requestBodyBytes
	}

	// Check if model field exists and get its value
	if model, exists := requestJSON["model"]; exists {
		// If model is already qwen3-coder-plus or qwen3-coder-flash, don't update it
		if model == "qwen3-coder-plus" || model == "qwen3-coder-flash" {
			return requestBodyBytes
		}
	}

	// Only update model if it's not one of the specified values
	requestJSON["model"] = "qwen3-coder-plus"
	modifiedBody, err := json.Marshal(requestJSON)
	if err != nil {
		return requestBodyBytes
	}
	return modifiedBody
}
