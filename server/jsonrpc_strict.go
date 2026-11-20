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
func validateProtocolMemberNames(message json.RawMessage) error {
	_, err := checkMembers(message, skipSpace(message, 0), jsonrpcEnvelopeMembers, "")
	return err
}

// checkMembers checks the member names of the object starting at b[i] and
// returns the index just past it. A value that is not an object is skipped.
func checkMembers(b []byte, i int, defined []string, path string) (int, error) {
	if i >= len(b) || b[i] != '{' {
		return skipValue(b, i), nil
	}
	var seen []string
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
		if path == "" && key == "params" {
			var err error
			if i, err = checkMembers(b, i, jsonrpcParamsMembers, "params."); err != nil {
				return 0, err
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
