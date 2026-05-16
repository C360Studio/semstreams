package graphindexspatial

// Regression tests for the polygon-query validation paths that previously
// called errs.WrapInvalid(nil, ...) and returned (nil, nil) — silently
// surfacing invalid input as an empty success reply on the NATS wire.
//
// See GH #94 for the underlying pkg/errs.Wrap*(nil, ...) footgun. This file
// asserts the call-site hotfix is in place; once #94 lands at the framework
// layer, the explicit errs.ErrInvalidData sentinel here remains valid and
// the tests continue to pass.

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
)

// The three sites the hotfix patched — each formerly passed nil as the inner
// error to errs.WrapInvalid, which returned nil, which the handler returned
// as (nil, nil), which the NATS wire encoded as an empty success reply.
func TestHandleQueryPolygonNATS_MissingPolygonFieldReturnsClassifiedError(t *testing.T) {
	c := &Component{}
	body := []byte(`{"limit": 10}`)

	data, err := c.handleQueryPolygonNATS(context.Background(), body)

	assertSilentSuccessRegression(t, "missing polygon", data, err)
	assertCarriesInvalidSentinel(t, "missing polygon", err)
}

func TestHandleQueryPolygonNATS_WrongGeometryTypeReturnsClassifiedError(t *testing.T) {
	c := &Component{}
	body := []byte(`{"polygon": {"type": "Point", "coordinates": [0, 0]}, "limit": 10}`)

	data, err := c.handleQueryPolygonNATS(context.Background(), body)

	assertSilentSuccessRegression(t, "wrong geometry type", data, err)
	assertCarriesInvalidSentinel(t, "wrong geometry type", err)
}

func TestHandleQueryPolygonNATS_EmptyPolygonCoordinatesReturnsClassifiedError(t *testing.T) {
	c := &Component{}
	body := []byte(`{"polygon": {"type": "Polygon", "coordinates": []}, "limit": 10}`)

	data, err := c.handleQueryPolygonNATS(context.Background(), body)

	assertSilentSuccessRegression(t, "empty polygon coordinates", data, err)
	assertCarriesInvalidSentinel(t, "empty polygon coordinates", err)
}

// Malformed JSON wraps the real json.Unmarshal error, not a sentinel. The
// (nil, nil) silent-success regression never affected this path — it's
// covered here only to prove the invariant that every validation failure
// out of this handler classifies as Invalid.
func TestHandleQueryPolygonNATS_MalformedJSONReturnsClassifiedError(t *testing.T) {
	c := &Component{}
	body := []byte(`{not valid json`)

	data, err := c.handleQueryPolygonNATS(context.Background(), body)

	assertSilentSuccessRegression(t, "malformed json envelope", data, err)
}

// assertSilentSuccessRegression encodes the contract the hotfix restores:
// validation failures must return a non-nil, classified-Invalid error AND
// nil data — never (nil, nil), which natsclient.SubscribeForRequests would
// encode as an empty success reply.
func assertSilentSuccessRegression(t *testing.T, label string, data []byte, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected non-nil error, got (nil, %v) — silent-success regression", label, data)
	}
	if data != nil {
		t.Errorf("%s: expected nil data on validation failure, got %q", label, data)
	}
	if !errs.IsInvalid(err) {
		t.Errorf("%s: expected errs.IsInvalid(err) == true, got class %q for err=%v",
			label, errs.Classify(err), err)
	}
}

// assertCarriesInvalidSentinel asserts the hotfix used errs.ErrInvalidData
// as the inner error rather than nil. Distinct from IsInvalid because the
// classification can be Invalid without the specific sentinel (e.g. wrapped
// json.Unmarshal errors). When GH #94 lands at the pkg/errs framework
// layer, this assertion remains correct — the explicit sentinel is still
// load-bearing for downstream errors.Is checks.
func assertCarriesInvalidSentinel(t *testing.T, label string, err error) {
	t.Helper()
	if !errors.Is(err, errs.ErrInvalidData) {
		t.Errorf("%s: expected errors.Is(err, errs.ErrInvalidData), got err=%v", label, err)
	}
}
