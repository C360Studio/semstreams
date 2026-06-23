// Package graphquery entity ID resolution
package graphquery

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// resolvePartialEntityID attempts to resolve a partial entity ID to a full 6-part ID.
// Resolution order:
// 1. If already a full ID (5+ dots), return as-is
// 2. Try alias lookup via graph.index.query.alias
// 3. Try suffix match via graph.ingest.query.suffix
func (c *Component) resolvePartialEntityID(ctx context.Context, partialID string) (string, error) {
	if partialID == "" {
		return "", nil
	}

	// Check if already a full entity ID (6 parts = 5 dots)
	if strings.Count(partialID, ".") >= 5 {
		return partialID, nil
	}

	// Try alias lookup first
	if fullID := c.resolveViaAlias(ctx, partialID); fullID != "" {
		c.logger.Debug("resolved entity ID via alias",
			"partial", partialID,
			"full", fullID)
		return fullID, nil
	}

	// Try suffix match
	if fullID := c.resolveViaSuffix(ctx, partialID); fullID != "" {
		c.logger.Debug("resolved entity ID via suffix",
			"partial", partialID,
			"full", fullID)
		return fullID, nil
	}

	c.logger.Debug("could not resolve partial entity ID",
		"partial", partialID)
	return "", nil
}

// resolveViaAlias tries to resolve an alias to a canonical entity ID.
func (c *Component) resolveViaAlias(ctx context.Context, alias string) string {
	subject := c.router.Route("alias")
	if subject == "" {
		return ""
	}

	reqData, err := json.Marshal(map[string]string{"alias": alias})
	if err != nil {
		return ""
	}

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	respData, err := c.natsClient.RequestClassified(queryCtx, subject, reqData, 2*time.Second)
	if err != nil {
		return ""
	}

	// Parse envelope response. Handler errors (invalid/internal) now
	// arrive via err above (ADR-060 PR-B), so the success body never
	// carries an error field on this path.
	var resp struct {
		Data struct {
			CanonicalID *string `json:"canonical_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return ""
	}

	if resp.Data.CanonicalID == nil {
		return ""
	}

	return *resp.Data.CanonicalID
}

// resolveViaSuffix tries to resolve a suffix pattern to a full entity ID.
// Uses the graph.ingest.query.suffix handler to find entities ending with the pattern.
func (c *Component) resolveViaSuffix(ctx context.Context, suffix string) string {
	reqData, err := json.Marshal(map[string]string{
		"suffix": suffix,
	})
	if err != nil {
		return ""
	}

	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// ADR-060: RequestClassified surfaces a handler failure via err instead of
	// an "error: <msg>" body that would mis-decode as an empty {id} (a silent
	// "no match"). On any error the resolver returns "" (no match) as before.
	respData, err := c.natsClient.RequestClassified(queryCtx, "graph.ingest.query.suffix", reqData, 2*time.Second)
	if err != nil {
		return ""
	}

	// Parse response - suffix handler returns {"id": "matched_id"}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respData, &resp); err != nil {
		return ""
	}

	return resp.ID
}
