package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// OTLPExporter implements Exporter by POSTing OTLP JSON to an HTTP endpoint.
// It avoids the OpenTelemetry SDK dependency by building the JSON wire format directly.
type OTLPExporter struct {
	client   *http.Client
	endpoint string
	headers  map[string]string
	logger   *slog.Logger
}

// NewOTLPExporter creates a new OTLP HTTP exporter.
// endpoint should be the base URL of the OTLP collector (e.g., "http://localhost:4318").
// insecure is accepted for future TLS configuration; the http.Client is always plain-HTTP capable.
func NewOTLPExporter(endpoint string, _ bool, headers map[string]string, logger *slog.Logger) *OTLPExporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &OTLPExporter{
		client:   &http.Client{},
		endpoint: endpoint,
		headers:  headers,
		logger:   logger,
	}
}

// ExportSpans marshals spans into OTLP JSON and POSTs to /v1/traces.
func (e *OTLPExporter) ExportSpans(ctx context.Context, spans []*SpanData) error {
	if len(spans) == 0 {
		return nil
	}

	body := buildOTLPTracePayload(spans)
	return e.post(ctx, "/v1/traces", body)
}

// ExportMetrics marshals metrics into OTLP JSON and POSTs to /v1/metrics.
func (e *OTLPExporter) ExportMetrics(ctx context.Context, metrics []*MetricData) error {
	if len(metrics) == 0 {
		return nil
	}

	body := buildOTLPMetricPayload(metrics)
	return e.post(ctx, "/v1/metrics", body)
}

// Shutdown closes idle connections on the underlying HTTP client.
func (e *OTLPExporter) Shutdown(_ context.Context) error {
	e.client.CloseIdleConnections()
	return nil
}

// post marshals v to JSON and POSTs it to endpoint+path.
func (e *OTLPExporter) post(ctx context.Context, path string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal OTLP payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("send OTLP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OTLP collector returned non-2xx status %d", resp.StatusCode)
	}

	return nil
}

// ---- OTLP JSON wire format construction ----

// otlpKeyValue is the OTLP attribute representation.
type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

// otlpAnyValue holds exactly one of the typed value fields.
type otlpAnyValue struct {
	StringValue *string  `json:"stringValue,omitempty"`
	IntValue    *int64   `json:"intValue,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
	BoolValue   *bool    `json:"boolValue,omitempty"`
}

// toOTLPAttrs converts a Go map to an OTLP key-value attribute slice.
func toOTLPAttrs(attrs map[string]any) []otlpKeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]otlpKeyValue, 0, len(attrs))
	for k, v := range attrs {
		kv := otlpKeyValue{Key: k}
		switch val := v.(type) {
		case string:
			kv.Value = otlpAnyValue{StringValue: &val}
		case int:
			i := int64(val)
			kv.Value = otlpAnyValue{IntValue: &i}
		case int64:
			kv.Value = otlpAnyValue{IntValue: &val}
		case float64:
			kv.Value = otlpAnyValue{DoubleValue: &val}
		case float32:
			f := float64(val)
			kv.Value = otlpAnyValue{DoubleValue: &f}
		case bool:
			kv.Value = otlpAnyValue{BoolValue: &val}
		default:
			s := fmt.Sprintf("%v", val)
			kv.Value = otlpAnyValue{StringValue: &s}
		}
		out = append(out, kv)
	}
	return out
}

// mapKindToInt converts a span kind string to the OTLP integer representation.
func mapKindToInt(kind string) int {
	switch kind {
	case "server":
		return 2
	case "client":
		return 3
	case "producer":
		return 4
	case "consumer":
		return 5
	default: // "internal" and anything else
		return 1
	}
}

// mapStatusCode converts status code strings to OTLP integer codes.
// OTLP: 0=Unset, 1=Ok, 2=Error
func mapStatusCode(code string) int {
	switch code {
	case "ok":
		return 1
	case "error":
		return 2
	default:
		return 0
	}
}

// otlpSpan is the OTLP span wire representation.
type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId,omitempty"`
	Name              string         `json:"name"`
	Kind              int            `json:"kind"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Status            otlpStatus     `json:"status"`
	Attributes        []otlpKeyValue `json:"attributes,omitempty"`
	Events            []otlpEvent    `json:"events,omitempty"`
}

// otlpStatus is the OTLP span status.
type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// otlpEvent is the OTLP span event.
type otlpEvent struct {
	TimeUnixNano string         `json:"timeUnixNano"`
	Name         string         `json:"name"`
	Attributes   []otlpKeyValue `json:"attributes,omitempty"`
}

// buildOTLPTracePayload constructs the resourceSpans JSON structure.
func buildOTLPTracePayload(spans []*SpanData) map[string]any {
	otlpSpans := make([]otlpSpan, 0, len(spans))
	for _, s := range spans {
		events := make([]otlpEvent, 0, len(s.Events))
		for _, ev := range s.Events {
			events = append(events, otlpEvent{
				TimeUnixNano: fmt.Sprintf("%d", ev.Timestamp.UnixNano()),
				Name:         ev.Name,
				Attributes:   toOTLPAttrs(ev.Attributes),
			})
		}

		otlpSpans = append(otlpSpans, otlpSpan{
			TraceID:           s.TraceID,
			SpanID:            s.SpanID,
			ParentSpanID:      s.ParentSpanID,
			Name:              s.Name,
			Kind:              mapKindToInt(s.Kind),
			StartTimeUnixNano: fmt.Sprintf("%d", s.StartTime.UnixNano()),
			EndTimeUnixNano:   fmt.Sprintf("%d", s.EndTime.UnixNano()),
			Status: otlpStatus{
				Code:    mapStatusCode(s.Status.Code),
				Message: s.Status.Message,
			},
			Attributes: toOTLPAttrs(s.Attributes),
			Events:     events,
		})
	}

	return map[string]any{
		"resourceSpans": []map[string]any{
			{
				"resource": map[string]any{
					"attributes": []map[string]any{},
				},
				"scopeSpans": []map[string]any{
					{
						"scope": map[string]any{
							"name": "semstreams",
						},
						"spans": otlpSpans,
					},
				},
			},
		},
	}
}

// otlpDataPoint is a single OTLP metric data point.
type otlpDataPoint struct {
	AsDouble   float64        `json:"asDouble,omitempty"`
	AsInt      int64          `json:"asInt,omitempty"`
	Attributes []otlpKeyValue `json:"attributes,omitempty"`
}

// buildOTLPMetricPayload constructs the resourceMetrics JSON structure.
func buildOTLPMetricPayload(metrics []*MetricData) map[string]any {
	otlpMetrics := make([]map[string]any, 0, len(metrics))
	for _, m := range metrics {
		dataPoints := make([]otlpDataPoint, 0, len(m.DataPoints))
		for _, dp := range m.DataPoints {
			dataPoints = append(dataPoints, otlpDataPoint{
				AsDouble:   dp.Value,
				Attributes: toOTLPAttrs(dp.Attributes),
			})
		}

		var dataField map[string]any
		switch m.Type {
		case MetricTypeCounter:
			dataField = map[string]any{
				"sum": map[string]any{
					"dataPoints":             dataPoints,
					"aggregationTemporality": 2, // AGGREGATION_TEMPORALITY_CUMULATIVE
					"isMonotonic":            true,
				},
			}
		case MetricTypeGauge:
			dataField = map[string]any{
				"gauge": map[string]any{
					"dataPoints": dataPoints,
				},
			}
		case MetricTypeHistogram:
			// Simplified histogram: use sum/count datapoints
			histPoints := make([]map[string]any, 0, len(m.DataPoints))
			for _, dp := range m.DataPoints {
				histPoints = append(histPoints, map[string]any{
					"count":      dp.Count,
					"sum":        dp.Sum,
					"attributes": toOTLPAttrs(dp.Attributes),
				})
			}
			dataField = map[string]any{
				"histogram": map[string]any{
					"dataPoints":             histPoints,
					"aggregationTemporality": 2,
				},
			}
		case MetricTypeSummary:
			summaryPoints := make([]map[string]any, 0, len(m.DataPoints))
			for _, dp := range m.DataPoints {
				quantiles := make([]map[string]any, 0, len(dp.Quantiles))
				for _, q := range dp.Quantiles {
					quantiles = append(quantiles, map[string]any{
						"quantile": q.Quantile,
						"value":    q.Value,
					})
				}
				summaryPoints = append(summaryPoints, map[string]any{
					"count":          dp.Count,
					"sum":            dp.Sum,
					"quantileValues": quantiles,
					"attributes":     toOTLPAttrs(dp.Attributes),
				})
			}
			dataField = map[string]any{
				"summary": map[string]any{
					"dataPoints": summaryPoints,
				},
			}
		default:
			dataField = map[string]any{
				"gauge": map[string]any{
					"dataPoints": dataPoints,
				},
			}
		}

		metric := map[string]any{
			"name":        m.Name,
			"description": m.Description,
			"unit":        m.Unit,
		}
		for k, v := range dataField {
			metric[k] = v
		}
		otlpMetrics = append(otlpMetrics, metric)
	}

	return map[string]any{
		"resourceMetrics": []map[string]any{
			{
				"resource": map[string]any{
					"attributes": []map[string]any{},
				},
				"scopeMetrics": []map[string]any{
					{
						"scope": map[string]any{
							"name": "semstreams",
						},
						"metrics": otlpMetrics,
					},
				},
			},
		},
	}
}
