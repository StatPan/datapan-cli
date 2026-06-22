package datago

import (
	"encoding/json"
	"fmt"
)

type ResponseEnvelope struct {
	OK         bool   `json:"ok"`
	Provider   string `json:"provider"`
	Dataset    string `json:"dataset"`
	Operation  string `json:"operation"`
	StatusCode int    `json:"status_code"`
	URL        string `json:"url"`
	Body       string `json:"body"`
}

func RowsFromJSON(data []byte) ([]map[string]any, error) {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("input is not JSON: %w", err)
	}
	rows := findRows(payload)
	if rows == nil {
		return nil, fmt.Errorf("could not find JSON rows; expected an array, {rows:[...]}, or data.go.kr response.body.items.item")
	}
	return rows, nil
}

func findRows(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		return objectRows(typed)
	case map[string]any:
		for _, path := range [][]string{
			{"rows"},
			{"results"},
			{"body"},
			{"response", "body", "items", "item"},
			{"response", "body", "items"},
		} {
			if rows := rowsAtPath(typed, path); rows != nil {
				return rows
			}
		}
		if body, ok := typed["body"].(string); ok {
			var nested any
			if err := json.Unmarshal([]byte(body), &nested); err == nil {
				return findRows(nested)
			}
		}
	}
	return nil
}

func rowsAtPath(root map[string]any, path []string) []map[string]any {
	var current any = root
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = obj[key]
	}
	switch typed := current.(type) {
	case []any:
		return objectRows(typed)
	case map[string]any:
		return []map[string]any{typed}
	default:
		return nil
	}
}

func objectRows(values []any) []map[string]any {
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		obj, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		rows = append(rows, obj)
	}
	return rows
}
