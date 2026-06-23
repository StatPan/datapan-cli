package datago

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ResponseEnvelope struct {
	OK             bool            `json:"ok"`
	Provider       string          `json:"provider"`
	Dataset        string          `json:"dataset"`
	Operation      string          `json:"operation"`
	StatusCode     int             `json:"status_code"`
	ContentType    string          `json:"content_type,omitempty"`
	SemanticStatus string          `json:"semantic_status"`
	Message        string          `json:"message,omitempty"`
	ProviderStatus *ProviderStatus `json:"provider_status,omitempty"`
	URL            string          `json:"url"`
	Body           string          `json:"body"`
}

type ProviderStatus struct {
	OK           bool   `json:"ok"`
	Source       string `json:"source"`
	Code         string `json:"code,omitempty"`
	Message      string `json:"message,omitempty"`
	ReasonCode   string `json:"reason_code,omitempty"`
	AuthMessage  string `json:"auth_message,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
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

func ClassifyResponse(statusCode int, contentType string, body []byte) (bool, string, string, *ProviderStatus) {
	if statusCode < 200 || statusCode >= 300 {
		return false, "http_error", fmt.Sprintf("HTTP %d", statusCode), nil
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return true, "empty_response", "", nil
	}
	lowerContentType := strings.ToLower(contentType)
	lowerBody := strings.ToLower(string(trimmed[:min(len(trimmed), 256)]))
	if strings.Contains(lowerContentType, "html") || strings.HasPrefix(lowerBody, "<!doctype html") || strings.HasPrefix(lowerBody, "<html") {
		return false, "html_response", "provider returned HTML instead of data", nil
	}
	if strings.Contains(lowerContentType, "json") || strings.HasPrefix(string(trimmed), "{") || strings.HasPrefix(string(trimmed), "[") {
		var payload any
		if err := json.Unmarshal(trimmed, &payload); err == nil {
			if status, ok := providerResult(payload); ok {
				if providerCodeOK(status.Code) {
					status.OK = true
					return true, "provider_ok", status.Message, &status
				}
				status.OK = false
				msg := status.Message
				if msg == "" {
					msg = "provider returned resultCode " + status.Code
				}
				return false, "provider_error", msg, &status
			}
			return true, "json_response", "", nil
		}
	}
	if status, ok := xmlProviderStatus(trimmed); ok {
		if providerCodeOK(status.Code) || providerCodeOK(status.ReasonCode) {
			status.OK = true
			return true, "provider_ok", status.Message, &status
		}
		status.OK = false
		msg := status.Message
		if msg == "" {
			msg = status.AuthMessage
		}
		if msg == "" && status.Code != "" {
			msg = "provider returned resultCode " + status.Code
		}
		if msg == "" && status.ReasonCode != "" {
			msg = "provider returned returnReasonCode " + status.ReasonCode
		}
		return false, "provider_error", msg, &status
	}
	if strings.HasPrefix(string(trimmed), "<") {
		return true, "xml_response", "", nil
	}
	return true, "unclassified_response", "", nil
}

func providerResult(value any) (ProviderStatus, bool) {
	switch typed := value.(type) {
	case map[string]any:
		code := stringValue(typed["resultCode"])
		msg := stringValue(typed["resultMsg"])
		if code != "" {
			return ProviderStatus{
				Source:  "resultCode/resultMsg",
				Code:    code,
				Message: msg,
			}, true
		}
		if header, ok := typed["header"]; ok {
			if status, ok := providerResult(header); ok {
				return status, ok
			}
		}
		if response, ok := typed["response"]; ok {
			if status, ok := providerResult(response); ok {
				return status, ok
			}
		}
		for _, child := range typed {
			if status, ok := providerResult(child); ok {
				return status, ok
			}
		}
	case []any:
		for _, child := range typed {
			if status, ok := providerResult(child); ok {
				return status, ok
			}
		}
	}
	return ProviderStatus{}, false
}

func providerCodeOK(code string) bool {
	code = strings.TrimSpace(strings.ToUpper(code))
	return code == "00" || code == "0" || code == "NORMAL_SERVICE"
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return ""
	}
}

func xmlTagValue(body []byte, tag string) string {
	pattern := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(tag) + `>\s*([^<]+?)\s*</` + regexp.QuoteMeta(tag) + `>`)
	match := pattern.FindSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(string(match[1]))
}

func xmlProviderStatus(body []byte) (ProviderStatus, bool) {
	if code := xmlTagValue(body, "resultCode"); code != "" {
		return ProviderStatus{
			Source:  "resultCode/resultMsg",
			Code:    code,
			Message: xmlTagValue(body, "resultMsg"),
		}, true
	}
	reasonCode := xmlTagValue(body, "returnReasonCode")
	authMessage := xmlTagValue(body, "returnAuthMsg")
	errorMessage := xmlTagValue(body, "errMsg")
	if reasonCode == "" && authMessage == "" && errorMessage == "" {
		return ProviderStatus{}, false
	}
	return ProviderStatus{
		Source:       "OpenAPI_ServiceResponse/cmmMsgHeader",
		ReasonCode:   reasonCode,
		AuthMessage:  authMessage,
		ErrorMessage: errorMessage,
		Message:      firstProviderMessage(authMessage, errorMessage),
	}, true
}

func firstProviderMessage(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
