package portgrammarcontrol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/c360studio/semstreams/internal/graphmutation"
)

type jsonReplacement struct {
	start int
	end   int
	data  []byte
}

func renderRewrittenDocument(source []byte, document any, items []WorkItem) ([]byte, error) {
	portPointers := map[string][]string{}
	for _, item := range items {
		segments := splitPointer(item.Pointer)
		if len(segments) < 2 {
			return nil, fmt.Errorf("invalid port row pointer %q", item.Pointer)
		}
		ports := segments[:len(segments)-2]
		portPointers[jsonPointer(ports)] = ports
	}
	replacements := make([]jsonReplacement, 0, len(portPointers))
	for _, pointer := range sortedKeys(portPointers) {
		segments := portPointers[pointer]
		value, err := getPointer(document, segments)
		if err != nil {
			return nil, err
		}
		start, end, err := locateJSONPointerSpan(source, segments)
		if err != nil {
			return nil, fmt.Errorf("locate %s: %w", pointer, err)
		}
		encoded, err := marshalIndentedValue(value)
		if err != nil {
			return nil, err
		}
		encoded = indentContinuation(encoded, sourceLineIndent(source, start))
		replacements = append(replacements, jsonReplacement{start: start, end: end, data: encoded})
	}
	sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
	result := append([]byte(nil), source...)
	for _, replacement := range replacements {
		if replacement.start < 0 || replacement.end < replacement.start || replacement.end > len(result) {
			return nil, fmt.Errorf("invalid replacement span %d:%d", replacement.start, replacement.end)
		}
		result = append(result[:replacement.start], append(replacement.data, result[replacement.end:]...)...)
	}
	return result, nil
}

func marshalIndentedValue(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func indentContinuation(data, indent []byte) []byte {
	return bytes.ReplaceAll(data, []byte{'\n'}, append([]byte{'\n'}, indent...))
}

func sourceLineIndent(data []byte, offset int) []byte {
	lineStart := bytes.LastIndexByte(data[:offset], '\n') + 1
	end := lineStart
	for end < offset && (data[end] == ' ' || data[end] == '\t') {
		end++
	}
	return append([]byte(nil), data[lineStart:end]...)
}

func locateJSONPointerSpan(data []byte, segments []string) (int, int, error) {
	base := 0
	current := data
	for _, segment := range segments {
		start, end, err := locateJSONChildSpan(current, segment)
		if err != nil {
			return 0, 0, err
		}
		base += start
		current = current[start:end]
	}
	return base, base + len(current), nil
}

func locateJSONChildSpan(data []byte, segment string) (int, int, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return 0, 0, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return 0, 0, fmt.Errorf("cannot descend through scalar")
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return 0, 0, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return 0, 0, fmt.Errorf("object key is %T", keyToken)
			}
			start := nextJSONValueStart(data, int(decoder.InputOffset()))
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return 0, 0, err
			}
			if key == segment {
				return start, int(decoder.InputOffset()), nil
			}
		}
		return 0, 0, fmt.Errorf("missing object key %q", segment)
	case '[':
		wanted, err := strconv.Atoi(segment)
		if err != nil || wanted < 0 {
			return 0, 0, fmt.Errorf("invalid array index %q", segment)
		}
		for index := 0; decoder.More(); index++ {
			start := nextJSONValueStart(data, int(decoder.InputOffset()))
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return 0, 0, err
			}
			if index == wanted {
				return start, int(decoder.InputOffset()), nil
			}
		}
		return 0, 0, fmt.Errorf("array index %d out of range", wanted)
	default:
		return 0, 0, fmt.Errorf("cannot descend through %q", delimiter)
	}
}

func nextJSONValueStart(data []byte, offset int) int {
	for offset < len(data) {
		switch data[offset] {
		case ' ', '\t', '\r', '\n', ':', ',':
			offset++
		default:
			return offset
		}
	}
	return offset
}

func mechanicalData(row map[string]any, kind string) (map[string]any, error) {
	data := map[string]any{}
	if nested, ok := row["config"].(map[string]any); ok {
		for key, value := range nested {
			data[key] = value
		}
	}
	for key, value := range row {
		switch key {
		case "name", "type", "required", "description", "config":
			continue
		}
		data[key] = value
	}
	if legacyInterface, ok := data["interface"].(string); ok {
		contract := map[string]any{"type": legacyInterface}
		if legacyInterface == graphmutation.InterfaceType {
			contract["version"] = graphmutation.InterfaceVersion
		}
		data["interface"] = contract
	}
	switch kind {
	case "jetstream":
		if subject := stringValue(data["subject"]); subject != "" {
			if _, exists := data["subjects"]; exists {
				return nil, fmt.Errorf("jetstream row declares both subject and subjects")
			}
			data["subjects"] = []any{subject}
			delete(data, "subject")
		}
		return data, nil
	case "kv-watch", "kv-write":
		bucket := stringValue(data["bucket"])
		if bucket == "" {
			bucket = stringValue(data["subject"])
		}
		if bucket == "" {
			return nil, fmt.Errorf("%s row has no bucket", kind)
		}
		result := map[string]any{"bucket": bucket}
		if value, ok := data["interface"]; ok {
			result["interface"] = value
		}
		return result, nil
	case "network":
		subject := stringValue(data["subject"])
		parsed, err := url.Parse(subject)
		if err != nil || parsed.Hostname() == "" || parsed.Port() == "" {
			return nil, fmt.Errorf("invalid network subject %q", subject)
		}
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			return nil, err
		}
		return map[string]any{"host": parsed.Hostname(), "port": port, "protocol": parsed.Scheme}, nil
	default:
		return data, nil
	}
}

func canonicalRow(row map[string]any, kind string, data map[string]any) map[string]any {
	result := map[string]any{"name": row["name"]}
	if value, ok := row["required"]; ok {
		result["required"] = value
	}
	if value, ok := row["description"]; ok {
		result["description"] = value
	}
	config := map[string]any{"kind": kind}
	for key, value := range data {
		config[key] = value
	}
	result["config"] = config
	return result
}

func jsonPointer(segments []string) string {
	if len(segments) == 0 {
		return ""
	}
	escaped := make([]string, len(segments))
	for index, segment := range segments {
		segment = strings.ReplaceAll(segment, "~", "~0")
		escaped[index] = strings.ReplaceAll(segment, "/", "~1")
	}
	return "/" + strings.Join(escaped, "/")
}

func splitPointer(pointer string) []string {
	if pointer == "" || pointer == "/" {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	for index, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		parts[index] = strings.ReplaceAll(part, "~0", "~")
	}
	return parts
}

func getPointer(root any, segments []string) (any, error) {
	current := root
	for _, segment := range segments {
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[segment]
			if !ok {
				return nil, fmt.Errorf("missing object key %q", segment)
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(value) {
				return nil, fmt.Errorf("invalid array index %q", segment)
			}
			current = value[index]
		default:
			return nil, fmt.Errorf("cannot descend through %T at %q", current, segment)
		}
	}
	return current, nil
}

func setPointer(root any, segments []string, replacement any) error {
	if len(segments) == 0 {
		return fmt.Errorf("cannot replace root")
	}
	parent, err := getPointer(root, segments[:len(segments)-1])
	if err != nil {
		return err
	}
	last := segments[len(segments)-1]
	switch value := parent.(type) {
	case map[string]any:
		value[last] = replacement
	case []any:
		index, err := strconv.Atoi(last)
		if err != nil || index < 0 || index >= len(value) {
			return fmt.Errorf("invalid array index %q", last)
		}
		value[index] = replacement
	default:
		return fmt.Errorf("cannot replace child of %T", parent)
	}
	return nil
}
