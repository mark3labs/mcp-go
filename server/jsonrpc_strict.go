package server

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Member names that encoding/json would also match as "Method", "paramſ" and
// so on. Keys that fold onto none of them are left alone.
var (
	jsonrpcEnvelopeMembers = []string{"jsonrpc", "method", "id", "params", "result", "error"}

	jsonrpcParamsMembers = []string{
		"name", "arguments", "uri", "cursor", "_meta", "protocolVersion",
		"capabilities", "clientInfo", "level", "ref", "argument", "requestId",
		"progressToken", "taskId", "reason", "requestState", "task", "context",
		"inputResponses", "notifications", "progress", "total", "message",
	}
)

// validateProtocolMemberNames rejects envelope and params member names that
// differ from the defined ones only by case or Unicode folding, and repeated
// members. It scans the bytes without decoding values. Malformed JSON is left
// to the caller's Unmarshal.
//
// protocolMethods is a list of method names defined by the protocol. For custom
// methods not in this list, params validation is skipped to allow applications
// to handle their own parameter schemas.
func validateProtocolMemberNames(message json.RawMessage, protocolMethods []string) error {
	_, err := checkMembers(message, skipSpace(message, 0), jsonrpcEnvelopeMembers, "", protocolMethods)
	return err
}

// checkMembers checks the member names of the object starting at b[i] and
// returns the index just past it. A value that is not an object is skipped.
// When called for the envelope (path == ""), it checks for a "method" member
// and only validates "params" if the method is in protocolMethods.
func checkMembers(b []byte, i int, defined []string, path string, protocolMethods []string) (int, error) {
	if i >= len(b) || b[i] != '{' {
		return skipValue(b, i), nil
	}
	var seen []string
	var currentMethod string
	i = skipSpace(b, i+1)
	for i < len(b) && b[i] == '"' {
		end := skipString(b, i)
		if end < 0 {
			return len(b), nil
		}
		key := string(b[i+1 : end-1])
		if strings.IndexByte(key, '\\') >= 0 {
			if json.Unmarshal(b[i:end], &key) != nil {
				return len(b), nil
			}
		}
		if err := checkMemberName(key, defined, seen, path); err != nil {
			return 0, err
		}
		// Tracking only defined names keeps this linear in the member count.
		if slices.Contains(defined, key) {
			seen = append(seen, key)
		}

		i = skipSpace(b, end)
		if i >= len(b) || b[i] != ':' {
			return len(b), nil
		}
		i = skipSpace(b, i+1)

		// At the envelope level (path == ""), track the method value
		if path == "" && key == "method" {
			methodEnd := skipString(b, i)
			if methodEnd > 0 {
				currentMethod = string(b[i+1 : methodEnd-1])
			}
		}

		// Only validate params members if this is a protocol-defined method
		if path == "" && key == "params" {
			shouldValidateParams := slices.Contains(protocolMethods, currentMethod)
			var err error
			if shouldValidateParams {
				if i, err = checkMembers(b, i, jsonrpcParamsMembers, "params.", protocolMethods); err != nil {
					return 0, err
				}
			} else {
				// Skip validation for custom methods; just skip the value
				i = skipValue(b, i)
			}
		} else {
			i = skipValue(b, i)
		}
		i = skipSpace(b, i)
		if i < len(b) && b[i] == ',' {
			i = skipSpace(b, i+1)
		}
	}
	return i + 1, nil
}

func checkMemberName(key string, defined, seen []string, path string) error {
	if slices.Contains(seen, key) {
		return fmt.Errorf("duplicate member %q", path+key)
	}
	if slices.Contains(defined, key) {
		return nil
	}
	for _, name := range defined {
		if strings.EqualFold(key, name) {
			return fmt.Errorf("member %q is not the defined name %q", path+key, path+name)
		}
	}
	return nil
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// skipString returns the index just past the string starting at b[i], or -1.
func skipString(b []byte, i int) int {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}

// skipValue returns the index just past the value starting at b[i].
func skipValue(b []byte, i int) int {
	depth := 0
	for ; i < len(b); i++ {
		switch b[i] {
		case '"':
			end := skipString(b, i)
			if end < 0 {
				return len(b)
			}
			i = end - 1
		case '{', '[':
			depth++
		case '}', ']':
			if depth == 0 {
				return i
			}
			depth--
		case ',':
			if depth == 0 {
				return i
			}
		}
		if depth == 0 && (b[i] == '"' || b[i] == '}' || b[i] == ']') {
			return i + 1
		}
	}
	return i
}

// protocolDefinedMethods returns a list of all method names defined by the
// MCP protocol. Custom methods not in this list will have their params
// validated separately by the application.
func protocolDefinedMethods() []string {
	return []string{
		"initialize",
		"ping",
		"server/discover",
		"resources/list",
		"resources/templates/list",
		"resources/read",
		"resources/subscribe",
		"resources/unsubscribe",
		"prompts/list",
		"prompts/get",
		"tools/list",
		"tools/call",
		"logging/setLevel",
		"elicitation/create",
		"notifications/elicitation/complete",
		"roots/list",
		"tasks/get",
		"tasks/list",
		"tasks/result",
		"tasks/cancel",
		"notifications/initialized",
		"notifications/cancelled",
		"notifications/progress",
		"notifications/message",
		"notifications/resources/list_changed",
		"notifications/resources/updated",
		"notifications/prompts/list_changed",
		"notifications/tools/list_changed",
		"notifications/roots/list_changed",
		"notifications/tasks/status",
		"completion/complete",
		"subscriptions/listen",
		"notifications/subscriptions/acknowledged",
		"sampling/createMessage",
	}
}
