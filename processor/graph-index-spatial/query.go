// Package graphindexspatial query handlers
package graphindexspatial

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/c360studio/semstreams/graph/geo/geojson"
	"github.com/c360studio/semstreams/pkg/errs"
)

// spatialFetchConcurrency is the max parallel KV Gets for spatial cell lookups.
const spatialFetchConcurrency = 10

// setupQueryHandlers sets up NATS request/reply subscriptions for query handlers
func (c *Component) setupQueryHandlers(ctx context.Context) error {
	// Subscribe to spatial bounds query
	boundsSub, err := c.natsClient.SubscribeForRequests(ctx, "graph.spatial.query.bounds", c.handleQueryBoundsNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe bounds query")
	}
	c.querySubscriptions = append(c.querySubscriptions, boundsSub)

	// Subscribe to spatial polygon-containment query (ADR-044 Phase 3).
	polySub, err := c.natsClient.SubscribeForRequests(ctx, "graph.spatial.query.polygon", c.handleQueryPolygonNATS)
	if err != nil {
		return errs.WrapTransient(err, "Component", "setupQueryHandlers", "subscribe polygon query")
	}
	c.querySubscriptions = append(c.querySubscriptions, polySub)

	c.logger.Info("query handlers registered",
		"subjects", []string{
			"graph.spatial.query.bounds",
			"graph.spatial.query.polygon",
		})

	return nil
}

// SpatialResult represents an entity found in spatial search
type SpatialResult struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// boundsRequest holds parsed spatial bounds query parameters
type boundsRequest struct {
	North float64 `json:"north"`
	South float64 `json:"south"`
	East  float64 `json:"east"`
	West  float64 `json:"west"`
	Limit int     `json:"limit"`
}

// handleQueryBoundsNATS handles spatial bounds queries via NATS request/reply
func (c *Component) handleQueryBoundsNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var req boundsRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryBoundsNATS", "invalid request")
	}

	if req.Limit <= 0 {
		req.Limit = 100
	}

	// Compute candidate cell keys from the bounding box.
	// Returns nil when the box is too large (> 10,000 cells) → fall back to full scan.
	candidateCells := geohashCellsInBounds(req.North, req.South, req.East, req.West, c.config.GeohashPrecision)

	var keys []string
	if candidateCells != nil {
		keys = candidateCells
	} else {
		var err error
		keys, err = c.spatialBucket.Keys(ctx)
		if err != nil {
			return json.Marshal([]SpatialResult{})
		}
	}

	results := c.collectSpatialResults(ctx, keys, req)
	return json.Marshal(results)
}

// spatialCellData holds the parsed content of a single spatial KV entry.
type spatialCellData struct {
	Entities map[string]struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
		Alt float64 `json:"alt"`
	} `json:"entities"`
}

// collectSpatialResults fetches spatial cells concurrently and filters entities within bounds.
func (c *Component) collectSpatialResults(ctx context.Context, keys []string, req boundsRequest) []SpatialResult {
	if len(keys) == 0 {
		return []SpatialResult{}
	}

	// Phase 1: Fetch all cells concurrently with bounded concurrency.
	type fetchResult struct {
		data spatialCellData
		ok   bool
	}
	fetched := make([]fetchResult, len(keys))
	sem := make(chan struct{}, spatialFetchConcurrency)
	var wg sync.WaitGroup

	for i, key := range keys {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(idx int, k string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			entry, err := c.spatialBucket.Get(ctx, k)
			if err != nil {
				return
			}
			var data spatialCellData
			if json.Unmarshal(entry.Value(), &data) == nil {
				fetched[idx] = fetchResult{data: data, ok: true}
			}
		}(i, key)
	}
	wg.Wait()

	// Phase 2: Filter entities within bounds (single-threaded, fast).
	results := make([]SpatialResult, 0)
	seen := make(map[string]bool)

	for _, fr := range fetched {
		if !fr.ok {
			continue
		}
		for entityID, coords := range fr.data.Entities {
			if seen[entityID] {
				continue
			}
			if coords.Lat >= req.South && coords.Lat <= req.North &&
				coords.Lon >= req.West && coords.Lon <= req.East {
				seen[entityID] = true
				results = append(results, SpatialResult{ID: entityID, Type: "entity"})
				if len(results) >= req.Limit {
					return results
				}
			}
		}
	}
	return results
}

// geohashMultiplier returns the bin-size multiplier for the given precision level.
// This mirrors the algorithm in component.go calculateGeohash so the two are
// always in sync.
func geohashMultiplier(precision int) float64 {
	switch precision {
	case 4:
		return 10.0
	case 5:
		return 50.0
	case 6:
		return 100.0
	case 7:
		return 300.0
	case 8:
		return 1000.0
	default:
		return 300.0
	}
}

// geohashCellsInBounds enumerates all geohash cell keys that fall within the
// given bounding box for the given precision level.
//
// The cell key format is "geo_<precision>_<latBin>_<lonBin>", matching the
// algorithm used by calculateGeohash in component.go.
//
// To prevent runaway allocations on very large bounding boxes (e.g. a global
// query) the function returns nil when the number of candidate cells would
// exceed 10,000. The caller must then fall back to a full key scan.
func geohashCellsInBounds(north, south, east, west float64, precision int) []string {
	const maxCells = 10_000

	multiplier := geohashMultiplier(precision)

	minLatBin := int(math.Floor((south + 90.0) * multiplier))
	maxLatBin := int(math.Floor((north + 90.0) * multiplier))
	minLonBin := int(math.Floor((west + 180.0) * multiplier))
	maxLonBin := int(math.Floor((east + 180.0) * multiplier))

	// Guard against degenerate or inverted bounds.
	if minLatBin > maxLatBin || minLonBin > maxLonBin {
		return nil
	}

	latCount := maxLatBin - minLatBin + 1
	lonCount := maxLonBin - minLonBin + 1
	if latCount*lonCount > maxCells {
		return nil // bounding box too large; caller falls back to full scan
	}

	cells := make([]string, 0, latCount*lonCount)
	for latBin := minLatBin; latBin <= maxLatBin; latBin++ {
		for lonBin := minLonBin; lonBin <= maxLonBin; lonBin++ {
			cells = append(cells, fmt.Sprintf("geo_%d_%d_%d", precision, latBin, lonBin))
		}
	}
	return cells
}

// polygonRequest holds parsed polygon-containment query parameters.
// The Polygon field carries the raw GeoJSON object so the handler
// can dispatch through geojson.UnmarshalGeometry (which validates
// the type discriminator) rather than trusting json.Unmarshal
// against a typed struct.
type polygonRequest struct {
	Polygon json.RawMessage `json:"polygon"`
	Limit   int             `json:"limit"`
}

// handleQueryPolygonNATS handles GeoJSON polygon-containment queries
// via NATS request/reply. The request body is JSON shaped as:
//
//	{
//	    "polygon": {"type": "Polygon", "coordinates": [[ [lon,lat], ... ]]},
//	    "limit":   100
//	}
//
// Entities are returned when their indexed point falls strictly
// inside the polygon's outer ring AND is not inside any hole.
// The polygon must be in WGS84 [longitude, latitude] coordinates
// per RFC 7946. Polygons spanning the antimeridian must be split
// at ±180 by the caller; this handler does not auto-split.
func (c *Component) handleQueryPolygonNATS(ctx context.Context, data []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var req polygonRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryPolygonNATS", "invalid request envelope")
	}
	if len(req.Polygon) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Component", "handleQueryPolygonNATS", "missing \"polygon\" field")
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}

	geom, err := geojson.UnmarshalGeometry(req.Polygon)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Component", "handleQueryPolygonNATS", "decode polygon")
	}
	// Phase 3 ships single-Polygon containment only. MultiPolygon
	// support (overlapping deployment areas / no-fly zones split
	// across multiple polygons) is a Phase 4+ extension —
	// callers needing that today must split client-side and
	// issue separate polygon queries, then UNION the results.
	poly, ok := geom.(geojson.Polygon)
	if !ok {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Component", "handleQueryPolygonNATS",
			fmt.Sprintf("expected Polygon geometry, got %q", geom.Type()))
	}
	poly = poly.Normalize().(geojson.Polygon)

	bbox := geojson.ComputeBBox(poly)
	if len(bbox) != 4 {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Component", "handleQueryPolygonNATS",
			"polygon has no coordinates")
	}

	// Reuse the existing geohash candidate-cell enumerator for
	// the polygon's bounding box. The polygon-containment filter
	// applied below tightens the bbox-coarse result.
	candidateCells := geohashCellsInBounds(bbox.North(), bbox.South(), bbox.East(), bbox.West(), c.config.GeohashPrecision)
	var keys []string
	if candidateCells != nil {
		keys = candidateCells
	} else {
		ks, err := c.spatialBucket.Keys(ctx)
		if err != nil {
			return json.Marshal([]SpatialResult{})
		}
		keys = ks
	}

	results := c.collectSpatialResultsByPolygon(ctx, keys, poly, req.Limit)
	return json.Marshal(results)
}

// collectSpatialResultsByPolygon fetches spatial cells concurrently
// and filters entities through Polygon.Contains. Structurally
// mirrors collectSpatialResults — kept separate rather than DRYed
// behind a filter callback because the filter shapes (rectangle vs
// polygon) and limit-checking are mixed concerns and a callback
// indirection would obscure the fast path.
func (c *Component) collectSpatialResultsByPolygon(ctx context.Context, keys []string, poly geojson.Polygon, limit int) []SpatialResult {
	if len(keys) == 0 {
		return []SpatialResult{}
	}

	type fetchResult struct {
		data spatialCellData
		ok   bool
	}
	fetched := make([]fetchResult, len(keys))
	sem := make(chan struct{}, spatialFetchConcurrency)
	var wg sync.WaitGroup

	for i, key := range keys {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(idx int, k string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			entry, err := c.spatialBucket.Get(ctx, k)
			if err != nil {
				return
			}
			var data spatialCellData
			if json.Unmarshal(entry.Value(), &data) == nil {
				fetched[idx] = fetchResult{data: data, ok: true}
			}
		}(i, key)
	}
	wg.Wait()

	cells := make([]spatialCellData, 0, len(fetched))
	for _, fr := range fetched {
		if fr.ok {
			cells = append(cells, fr.data)
		}
	}
	return filterEntitiesByPolygon(cells, poly, limit)
}

// filterEntitiesByPolygon iterates the entities across the given
// cells and returns those whose indexed point falls inside the
// polygon. Limit truncates the result; pass <=0 for "no limit".
//
// Pure function — no NATS / context / IO — so it is the focal
// point for polygon-containment unit tests.
func filterEntitiesByPolygon(cells []spatialCellData, poly geojson.Polygon, limit int) []SpatialResult {
	results := make([]SpatialResult, 0)
	seen := make(map[string]bool)
	for _, data := range cells {
		for entityID, coords := range data.Entities {
			if seen[entityID] {
				continue
			}
			// GeoJSON stores [lon, lat]; the spatial index
			// stores them in named fields. Construct the
			// Position with lon first to match RFC 7946 order.
			point := geojson.NewPosition(coords.Lon, coords.Lat)
			if !poly.Contains(point) {
				continue
			}
			seen[entityID] = true
			results = append(results, SpatialResult{ID: entityID, Type: "entity"})
			if limit > 0 && len(results) >= limit {
				return results
			}
		}
	}
	return results
}
