// Package portgrammarcontrol validates the retained Foundation B migration record.
// It is internal test support, not a framework port API.
package portgrammarcontrol

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// WorklistPath names the immutable Foundation B migration worklist.
	WorklistPath = "docs/proposals/foundation-b-port-language-worklist.tsv"
	// DispositionsPath names the immutable reviewed disposition ledger.
	DispositionsPath = "docs/proposals/foundation-b-port-language-dispositions.tsv"
)

var worklistHeader = []string{
	"record_id", "record_type", "path", "pointer", "enclosing", "lane", "ordinal",
	"name", "current_kind", "current_data", "classification", "source_line", "source_column", "source_sha256",
}

var dispositionHeader = []string{
	"record_id", "path", "pointer", "action", "target_lane", "target_kind", "target_data", "reason",
}

// WorkItem identifies one frozen configuration row or Go port construction.
type WorkItem struct {
	RecordID       string
	RecordType     string
	Path           string
	Pointer        string
	Enclosing      string
	Lane           string
	Ordinal        int
	Name           string
	CurrentKind    string
	CurrentData    string
	Classification string
	SourceLine     int
	SourceColumn   int
	SourceSHA256   string
}

// Disposition records the reviewed target for one adjudicated configuration row.
type Disposition struct {
	RecordID   string
	Path       string
	Pointer    string
	Action     string
	TargetLane string
	TargetKind string
	TargetData string
	Reason     string
}

// Plan combines the immutable worklist with its reviewed dispositions.
type Plan struct {
	Items        []WorkItem
	Dispositions map[string]Disposition
}

// DispositionCounts reports the reviewed legacy-kind population.
type DispositionCounts struct {
	KV      int
	KVRead  int
	HTTP    int
	Deleted int
}

// LoadPlan reads and validates the immutable control artifacts under root.
func LoadPlan(root string) (*Plan, error) {
	items, err := readWorklist(filepath.Join(root, WorklistPath))
	if err != nil {
		return nil, err
	}
	dispositions, err := readDispositions(filepath.Join(root, DispositionsPath))
	if err != nil {
		return nil, err
	}
	plan := &Plan{Items: items, Dispositions: dispositions}
	if err := plan.ValidateLegacyPhase(); err != nil {
		return nil, err
	}
	if _, err := plan.ValidateDispositions(); err != nil {
		return nil, err
	}
	return plan, nil
}

// ValidateLegacyPhase verifies the accepted historical population and classifications.
func (p *Plan) ValidateLegacyPhase() error {
	configItems := p.ConfigItems()
	adjudicated := 0
	kinds := map[string]int{}
	for _, item := range configItems {
		switch item.Classification {
		case "mechanical":
		case "adjudicated":
			adjudicated++
			kinds[item.CurrentKind]++
		default:
			return fmt.Errorf("config item %s has invalid legacy classification %q", item.RecordID, item.Classification)
		}
	}
	for _, item := range p.GoItems() {
		if item.Classification != "go-construction" {
			return fmt.Errorf("Go item %s has invalid legacy classification %q", item.RecordID, item.Classification)
		}
	}
	checks := []struct {
		label string
		got   int
		want  int
	}{
		{"total identities", len(p.Items), 646},
		{"config rows", len(configItems), 522},
		{"config documents", p.ConfigDocumentCount(), 24},
		{"mechanical rows", p.MechanicalCount(), 448},
		{"adjudicated rows", adjudicated, 74},
		{"kv adjudications", kinds["kv"], 57},
		{"kv-read adjudications", kinds["kv-read"], 9},
		{"http adjudications", kinds["http"], 8},
		{"Go constructions", len(p.GoItems()), 124},
		{"Go files", p.GoFileCount(), 34},
		{"Go enclosing sources", p.GoSourceCount(), 41},
	}
	for _, check := range checks {
		if check.got != check.want {
			return fmt.Errorf("legacy population mismatch: %s=%d, want %d", check.label, check.got, check.want)
		}
	}
	return nil
}

// ConfigItems returns the frozen configuration-row work items.
func (p *Plan) ConfigItems() []WorkItem { return filterItems(p.Items, "config") }

// GoItems returns the frozen executable Go construction work items.
func (p *Plan) GoItems() []WorkItem { return filterItems(p.Items, "go") }

func filterItems(items []WorkItem, kind string) []WorkItem {
	result := make([]WorkItem, 0)
	for _, item := range items {
		if item.RecordType == kind {
			result = append(result, item)
		}
	}
	return result
}

// MechanicalCount returns the number of configuration rows requiring no judgment.
func (p *Plan) MechanicalCount() int {
	count := 0
	for _, item := range p.ConfigItems() {
		if item.Classification == "mechanical" {
			count++
		}
	}
	return count
}

// ConfigPaths returns the sorted unique configuration document paths.
func (p *Plan) ConfigPaths() []string {
	set := map[string]struct{}{}
	for _, item := range p.ConfigItems() {
		set[item.Path] = struct{}{}
	}
	return sortedKeys(set)
}

// ConfigDocumentCount returns the number of frozen configuration documents.
func (p *Plan) ConfigDocumentCount() int { return len(p.ConfigPaths()) }

// GoFileCount returns the number of production files containing frozen constructions.
func (p *Plan) GoFileCount() int {
	set := map[string]struct{}{}
	for _, item := range p.GoItems() {
		set[item.Path] = struct{}{}
	}
	return len(set)
}

// GoSourceCount returns the number of enclosing functions or methods in the Go census.
func (p *Plan) GoSourceCount() int {
	set := map[string]struct{}{}
	for _, item := range p.GoItems() {
		set[item.Path+"#"+item.Enclosing] = struct{}{}
	}
	return len(set)
}

func indexItems(items []WorkItem) (map[string]WorkItem, error) {
	result := make(map[string]WorkItem, len(items))
	for _, item := range items {
		if item.RecordID == "" {
			return nil, errors.New("blank record_id")
		}
		if _, exists := result[item.RecordID]; exists {
			return nil, fmt.Errorf("duplicate record_id %s", item.RecordID)
		}
		result[item.RecordID] = item
	}
	return result, nil
}

// ValidateDispositions verifies exact reviewed targets for all adjudicated rows.
func (p *Plan) ValidateDispositions() (DispositionCounts, error) {
	return p.validateDispositions(true)
}

func (p *Plan) validateDispositions(requireFrozenCounts bool) (DispositionCounts, error) {
	var counts DispositionCounts
	adjudicated := map[string]WorkItem{}
	for _, item := range p.ConfigItems() {
		if item.Classification == "adjudicated" {
			adjudicated[item.RecordID] = item
		}
	}
	if requireFrozenCounts && len(adjudicated) != 74 {
		return counts, fmt.Errorf("adjudicated work items = %d, want 74", len(adjudicated))
	}
	if len(p.Dispositions) != len(adjudicated) {
		return counts, fmt.Errorf("dispositions = %d, adjudicated work items = %d", len(p.Dispositions), len(adjudicated))
	}
	for id, disposition := range p.Dispositions {
		item, ok := adjudicated[id]
		if !ok {
			return counts, fmt.Errorf("disposition has no adjudicated work item: %s", id)
		}
		if disposition.Path != item.Path || disposition.Pointer != item.Pointer {
			return counts, fmt.Errorf("disposition identity mismatch for %s", id)
		}
		if disposition.Action == "" || disposition.TargetLane == "" || disposition.TargetKind == "" || disposition.TargetData == "" || disposition.Reason == "" {
			return counts, fmt.Errorf("disposition %s has blank required field", id)
		}
		var target map[string]any
		if err := json.Unmarshal([]byte(disposition.TargetData), &target); err != nil || len(target) == 0 {
			return counts, fmt.Errorf("disposition %s target_data must be a nonempty JSON object", id)
		}
		var current map[string]any
		if err := json.Unmarshal([]byte(item.CurrentData), &current); err != nil {
			return counts, fmt.Errorf("decode frozen row %s: %w", id, err)
		}
		switch item.CurrentKind {
		case "kv":
			counts.KV++
			bucket := stringValue(current["bucket"])
			if bucket == "" {
				bucket = stringValue(current["subject"])
			}
			if disposition.Action != "rewrite" || disposition.TargetLane != item.Lane || disposition.TargetKind != "kv-write" || len(target) != 1 || stringValue(target["bucket"]) != bucket || disposition.Reason != "reviewed-current-output-writes-kv-resource" {
				return counts, fmt.Errorf("invalid kv disposition %s", id)
			}
		case "kv-read":
			counts.KVRead++
			if disposition.Action == "delete" {
				counts.Deleted++
				if item.Enclosing != "graph-query" || disposition.TargetLane != "<deleted>" || disposition.TargetKind != "<deleted>" || len(target) != 1 || stringValue(target["reason"]) != "no-runtime-consumer" || disposition.Reason != "reviewed-dead-graph-query-entity-states-row" {
					return counts, fmt.Errorf("invalid deletion disposition %s", id)
				}
			} else if disposition.Action != "rewrite" || disposition.TargetLane != "inputs" || disposition.TargetKind != "kv-read" || len(target) != 1 || stringValue(target["bucket"]) != stringValue(current["bucket"]) || disposition.Reason != "reviewed-agentic-tools-exact-read-input" {
				return counts, fmt.Errorf("invalid kv-read disposition %s", id)
			}
		case "http":
			counts.HTTP++
			host, port, err := parseListenSubject(stringValue(current["subject"]))
			if err != nil {
				return counts, fmt.Errorf("invalid frozen http row %s: %w", id, err)
			}
			if disposition.Action != "rewrite" || disposition.TargetLane != item.Lane || disposition.TargetKind != "network" || len(target) != 3 || stringValue(target["protocol"]) != "http" || stringValue(target["host"]) != host || jsonNumberInt(target["port"]) != port || disposition.Reason != "reviewed-http-listener-network-protocol" {
				return counts, fmt.Errorf("invalid http disposition %s", id)
			}
		default:
			return counts, fmt.Errorf("unexpected adjudicated kind %q for %s", item.CurrentKind, id)
		}
	}
	return counts, nil
}

func readWorklist(path string) ([]WorkItem, error) {
	records, err := readTSV(path, worklistHeader)
	if err != nil {
		return nil, err
	}
	items := make([]WorkItem, 0, len(records))
	for _, row := range records {
		ordinal, err := strconv.Atoi(row[6])
		if err != nil {
			return nil, fmt.Errorf("parse worklist ordinal for %s: %w", row[0], err)
		}
		line, err := strconv.Atoi(row[11])
		if err != nil {
			return nil, fmt.Errorf("parse worklist line for %s: %w", row[0], err)
		}
		column, err := strconv.Atoi(row[12])
		if err != nil {
			return nil, fmt.Errorf("parse worklist column for %s: %w", row[0], err)
		}
		items = append(items, WorkItem{row[0], row[1], row[2], row[3], row[4], row[5], ordinal, row[7], row[8], row[9], row[10], line, column, row[13]})
	}
	if _, err := indexItems(items); err != nil {
		return nil, err
	}
	return items, nil
}

func readDispositions(path string) (map[string]Disposition, error) {
	records, err := readTSV(path, dispositionHeader)
	if err != nil {
		return nil, err
	}
	result := make(map[string]Disposition, len(records))
	for _, row := range records {
		item := Disposition{row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7]}
		if _, exists := result[item.RecordID]; exists {
			return nil, fmt.Errorf("duplicate disposition %s", item.RecordID)
		}
		result[item.RecordID] = item
	}
	return result, nil
}

func readTSV(path string, expectedHeader []string) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.Comma = '\t'
	reader.Comment = '#'
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(records) == 0 || !equalStrings(records[0], expectedHeader) {
		return nil, fmt.Errorf("%s has unexpected header", path)
	}
	for i, record := range records[1:] {
		if len(record) != len(expectedHeader) {
			return nil, fmt.Errorf("%s row %d has %d columns, want %d", path, i+2, len(record), len(expectedHeader))
		}
	}
	return records[1:], nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortedKeys[V any](set map[string]V) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func stringValue(value any) string {
	valueString, _ := value.(string)
	return valueString
}

func jsonNumberInt(value any) int {
	switch number := value.(type) {
	case json.Number:
		result, _ := strconv.Atoi(number.String())
		return result
	case float64:
		return int(number)
	case int:
		return number
	default:
		return 0
	}
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
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
