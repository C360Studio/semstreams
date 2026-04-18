package executors

import "log/slog"

// registerHTTPRequest registers the http_request executor globally.
// Always available; no external deps.
func registerHTTPRequest(logger *slog.Logger) {
	http := NewHTTPRequestExecutor()
	if err := registerGlobal("http_request", http); err != nil {
		logger.Warn("Failed to register http_request tool", slog.Any("error", err))
		return
	}
	logger.Info("Registered http_request tool (global)")
}
