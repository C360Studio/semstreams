package portgrammarcontrol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// RewriteOptions selects preview, verification, and reviewed-disposition behavior.
type RewriteOptions struct {
	DryRun            bool
	Check             bool
	ApplyDispositions bool
}

// Output contains one deterministic rewritten configuration document.
type Output struct {
	Path   string
	SHA256 string
	Data   []byte
}

func parseListenSubject(subject string) (string, int, error) {
	if subject == "" {
		return "", 0, fmt.Errorf("blank listen subject")
	}
	value := subject
	if strings.HasPrefix(value, ":") {
		value = "0.0.0.0" + value
	}
	parsed, err := url.Parse("http://" + value)
	if err != nil {
		return "", 0, fmt.Errorf("parse listen subject %q: %w", subject, err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("invalid listen port in %q", subject)
	}
	host := parsed.Hostname()
	if host == "" {
		host = "0.0.0.0"
	}
	return host, port, nil
}

// Rewrite transforms only frozen worklist rows into a safe external output root.
func Rewrite(root, outputRoot string, plan *Plan, options RewriteOptions) ([]Output, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil rewrite plan")
	}
	if !options.DryRun && outputRoot == "" {
		return nil, fmt.Errorf("output root is required unless dry-run is selected")
	}
	if options.Check && options.DryRun {
		return nil, fmt.Errorf("check and dry-run are mutually exclusive")
	}
	if !options.DryRun {
		if err := validateOutputRoot(root, outputRoot, options.Check); err != nil {
			return nil, err
		}
	}
	if _, err := plan.validateDispositions(false); err != nil {
		return nil, err
	}
	itemsByPath := map[string][]WorkItem{}
	for _, item := range plan.ConfigItems() {
		itemsByPath[item.Path] = append(itemsByPath[item.Path], item)
	}
	paths := sortedKeys(itemsByPath)
	outputs := make([]Output, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return nil, err
		}
		document, err := decodeJSON(data)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		if err := validateFrozenRows(document, itemsByPath[path]); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := applyRewrites(document, itemsByPath[path], plan.Dispositions, options.ApplyDispositions); err != nil {
			return nil, fmt.Errorf("rewrite %s: %w", path, err)
		}
		encoded, err := json.MarshalIndent(document, "", "  ")
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, '\n')
		output := Output{Path: path, SHA256: sha256Hex(encoded), Data: encoded}
		outputs = append(outputs, output)
		if options.DryRun {
			continue
		}
		target := filepath.Join(outputRoot, filepath.FromSlash(path))
		if options.Check {
			existing, err := os.ReadFile(target)
			if err != nil {
				return nil, fmt.Errorf("check output %s: %w", target, err)
			}
			if !bytes.Equal(existing, encoded) {
				return nil, fmt.Errorf("check output %s differs from deterministic rewrite", target)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, encoded, 0o644); err != nil {
			return nil, err
		}
	}
	return outputs, nil
}

func validateOutputRoot(root, outputRoot string, check bool) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootCanonical, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	outputAbs, err := filepath.Abs(outputRoot)
	if err != nil {
		return err
	}
	if pathsOverlap(rootCanonical, outputAbs) {
		return fmt.Errorf("output root %s overlaps repository %s", outputAbs, rootCanonical)
	}

	info, statErr := os.Lstat(outputAbs)
	if statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output root must not be a symlink: %s", outputAbs)
		}
		if !info.IsDir() {
			return fmt.Errorf("output root is not a directory: %s", outputAbs)
		}
		canonical, err := filepath.EvalSymlinks(outputAbs)
		if err != nil {
			return err
		}
		if pathsOverlap(rootCanonical, canonical) {
			return fmt.Errorf("canonical output root %s overlaps repository %s", canonical, rootCanonical)
		}
		if check {
			return nil
		}
		entries, err := os.ReadDir(outputAbs)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("rewrite output root must be empty: %s", outputAbs)
		}
		return nil
	}
	if !os.IsNotExist(statErr) {
		return statErr
	}
	if check {
		return fmt.Errorf("check output root does not exist: %s", outputAbs)
	}

	existing := filepath.Dir(outputAbs)
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return fmt.Errorf("no existing parent for output root %s", outputAbs)
		}
		existing = parent
	}
	canonicalParent, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	remainder, err := filepath.Rel(existing, outputAbs)
	if err != nil {
		return err
	}
	canonicalCandidate := filepath.Join(canonicalParent, remainder)
	if pathsOverlap(rootCanonical, canonicalCandidate) {
		return fmt.Errorf("canonical output root %s overlaps repository %s", canonicalCandidate, rootCanonical)
	}
	return nil
}

func pathsOverlap(left, right string) bool {
	return pathWithin(left, right) || pathWithin(right, left)
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

type rewriteOperation struct {
	item        WorkItem
	targetLane  string
	replacement map[string]any
	delete      bool
}

type rewriteMove struct {
	recordID    string
	target      []string
	replacement map[string]any
}

func validateFrozenRows(document any, items []WorkItem) error {
	for _, item := range items {
		value, err := getPointer(document, splitPointer(item.Pointer))
		if err != nil {
			return fmt.Errorf("locate %s: %w", item.RecordID, err)
		}
		compact, err := compactJSON(value)
		if err != nil {
			return err
		}
		if compact != item.CurrentData || sha256Hex([]byte(compact)) != item.SourceSHA256 {
			return fmt.Errorf("frozen row changed at %s", item.RecordID)
		}
	}
	return nil
}

func applyRewrites(document any, items []WorkItem, dispositions map[string]Disposition, applyDispositions bool) error {
	byLane := map[string]map[int]rewriteOperation{}
	laneSegments := map[string][]string{}
	for _, item := range items {
		segments := splitPointer(item.Pointer)
		value, err := getPointer(document, segments)
		if err != nil {
			return err
		}
		row, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is not an object", item.RecordID)
		}
		operation := rewriteOperation{item: item, targetLane: item.Lane}
		if item.Classification == "adjudicated" {
			if !applyDispositions {
				continue
			}
			disposition := dispositions[item.RecordID]
			if disposition.Action == "delete" {
				operation.delete = true
			} else {
				operation.targetLane = disposition.TargetLane
				var targetData map[string]any
				if err := json.Unmarshal([]byte(disposition.TargetData), &targetData); err != nil {
					return err
				}
				operation.replacement = canonicalRow(row, disposition.TargetKind, targetData)
			}
		} else {
			targetKind := item.CurrentKind
			if item.Lane == "kv_write" {
				operation.targetLane = "outputs"
			}
			targetData, err := mechanicalData(row, targetKind)
			if err != nil {
				return fmt.Errorf("%s: %w", item.RecordID, err)
			}
			operation.replacement = canonicalRow(row, targetKind, targetData)
		}
		lane := jsonPointer(segments[:len(segments)-1])
		if byLane[lane] == nil {
			byLane[lane] = map[int]rewriteOperation{}
			laneSegments[lane] = segments[:len(segments)-1]
		}
		byLane[lane][item.Ordinal] = operation
	}

	var moves []rewriteMove
	for _, lane := range sortedKeys(byLane) {
		segments := laneSegments[lane]
		value, err := getPointer(document, segments)
		if err != nil {
			return err
		}
		rows, ok := value.([]any)
		if !ok {
			return fmt.Errorf("lane %s is %T, want array", lane, value)
		}
		rewritten := make([]any, 0, len(rows))
		for index, row := range rows {
			operation, exists := byLane[lane][index]
			if !exists {
				rewritten = append(rewritten, row)
				continue
			}
			if operation.delete {
				continue
			}
			if operation.targetLane == operation.item.Lane {
				rewritten = append(rewritten, operation.replacement)
				continue
			}
			target := append([]string(nil), segments[:len(segments)-1]...)
			moves = append(moves, rewriteMove{operation.item.RecordID, append(target, operation.targetLane), operation.replacement})
		}
		if err := setPointer(document, segments, rewritten); err != nil {
			return err
		}
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].recordID < moves[j].recordID })
	for _, move := range moves {
		parent, err := getPointer(document, move.target[:len(move.target)-1])
		if err != nil {
			return err
		}
		ports, ok := parent.(map[string]any)
		if !ok {
			return fmt.Errorf("move target parent is %T, want object", parent)
		}
		lane := move.target[len(move.target)-1]
		rows, _ := ports[lane].([]any)
		ports[lane] = append(rows, move.replacement)
	}
	return nil
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
	switch kind {
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

// MarshalOutputs renders stable path and digest lines for command output.
func MarshalOutputs(outputs []Output) []byte {
	copyOutputs := append([]Output(nil), outputs...)
	sort.Slice(copyOutputs, func(i, j int) bool { return copyOutputs[i].Path < copyOutputs[j].Path })
	var builder strings.Builder
	for _, output := range copyOutputs {
		fmt.Fprintf(&builder, "%s\t%s\n", output.Path, output.SHA256)
	}
	return []byte(builder.String())
}
