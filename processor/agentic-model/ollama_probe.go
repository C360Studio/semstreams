package agenticmodel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ollamaSetupGuideRef points users at the Modelfile recipe for raising
// num_ctx on a local Ollama model. Ollama's /v1/ compat layer rejects
// num_ctx in requests (PR #6137 closed on principle), so the fix has to
// live on the server side via `ollama create -f Modelfile`.
const ollamaSetupGuideRef = "docs/operations/04-ollama-setup.md"

// ollamaDefaultNumCtx is the historical Ollama default when no num_ctx is
// set — used as the silent-truncation threshold when /api/show reports no
// explicit num_ctx on the model.
const ollamaDefaultNumCtx = 4096

// ollamaProbeTimeout caps the /api/show round-trip so probe failures never
// block the first real ChatCompletion.
const ollamaProbeTimeout = 3 * time.Second

// ollamaShowResponse captures the /api/show fields we care about. Ollama
// returns "parameters" as a text block — one "<key> <value>" per line —
// reflecting what the server will actually apply, not the model
// architecture's upper bound (that lives in model_info.context_length).
type ollamaShowResponse struct {
	Parameters string `json:"parameters"`
}

// runProviderStartupProbes dispatches one-time provider-specific checks that
// warn about silent-failure traps. Currently only Ollama needs one — its /v1/
// compat layer can't override num_ctx per-request, so we probe /api/show once
// and warn on mismatch. Safe to call on every request; sync.Once gates the work.
func (c *Client) runProviderStartupProbes(ctx context.Context) {
	if c.endpoint == nil || c.endpoint.Provider != "ollama" {
		return
	}
	c.ollamaProbeOnce.Do(func() { c.probeOllamaNumCtx(ctx) })
}

// probeOllamaNumCtx hits /api/show once per Client and logs a WARN when the
// model's num_ctx is below endpoint.MaxTokens. Ollama's /v1/chat/completions
// has no way to override num_ctx per-request, so a mismatch means every
// prompt beyond the server's num_ctx truncates silently on the server side.
//
// All failure modes (Ollama down, 404, parse errors) fall to Debug — a probe
// failure must not block the real request. The caller is expected to guard
// this with sync.Once so the WARN fires at most once per endpoint.
func (c *Client) probeOllamaNumCtx(ctx context.Context) {
	if c.endpoint == nil || c.logger == nil {
		return
	}
	baseURL := ollamaBaseURL(c.endpoint.URL)
	if baseURL == "" {
		return
	}

	body, err := json.Marshal(map[string]string{"name": c.endpoint.Model})
	if err != nil {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, ollamaProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.logger.Debug("ollama num_ctx probe: /api/show unreachable",
			slog.String("url", baseURL+"/api/show"),
			slog.String("model", c.endpoint.Model),
			slog.String("error", err.Error()))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		c.logger.Debug("ollama num_ctx probe: /api/show non-200",
			slog.Int("status", resp.StatusCode),
			slog.String("model", c.endpoint.Model))
		return
	}

	var show ollamaShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&show); err != nil {
		c.logger.Debug("ollama num_ctx probe: decode failed",
			slog.String("model", c.endpoint.Model),
			slog.String("error", err.Error()))
		return
	}

	modelNumCtx, explicit := parseOllamaNumCtx(show.Parameters)
	if !explicit {
		modelNumCtx = ollamaDefaultNumCtx
	}

	if c.endpoint.MaxTokens > 0 && modelNumCtx < c.endpoint.MaxTokens {
		c.logger.Warn("ollama model num_ctx is below endpoint.max_tokens — prompts will silently truncate on the server",
			slog.String("model", c.endpoint.Model),
			slog.Int("model_num_ctx", modelNumCtx),
			slog.Bool("num_ctx_explicit", explicit),
			slog.Int("endpoint_max_tokens", c.endpoint.MaxTokens),
			slog.String("fix", ollamaSetupGuideRef))
	}
}

// ollamaBaseURL strips the OpenAI-compat "/v1" suffix from an endpoint URL so
// we can reach the native /api/show endpoint. Empty input yields empty output
// so callers can skip the probe when URL is unset.
func ollamaBaseURL(url string) string {
	u := strings.TrimRight(url, "/")
	return strings.TrimSuffix(u, "/v1")
}

// parseOllamaNumCtx pulls num_ctx out of /api/show's "parameters" block,
// which looks like: "num_ctx 32768\nstop \"<|eot|>\"\n...". Returns
// (0, false) when num_ctx is absent — the caller treats that as Ollama's
// built-in default, which is the common silent-truncation case.
func parseOllamaNumCtx(params string) (int, bool) {
	scanner := bufio.NewScanner(strings.NewReader(params))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "num_ctx") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		return n, true
	}
	return 0, false
}
