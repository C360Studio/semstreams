// Package client provides HTTP clients for SemStreams E2E tests
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/config"
)

// ObservabilityClient interacts with SemStreams component management endpoints
type ObservabilityClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewObservabilityClient creates a new client for SemStreams observability endpoints
func NewObservabilityClient(baseURL string) *ObservabilityClient {
	return &ObservabilityClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: config.DefaultTestConfig.Timeout,
		},
	}
}

// PlatformHealth represents overall platform health status
type PlatformHealth struct {
	Healthy bool   `json:"healthy"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ComponentInfo represents a single component's information
// Matches SemStreams /components/list API response format
type ComponentInfo struct {
	Name      string `json:"name"`
	Component string `json:"component"` // Component factory name (e.g., "udp", "graph-processor")
	Type      string `json:"type"`      // Component category (input/processor/output/storage/gateway)
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Healthy   bool   `json:"healthy"`
	LastError string `json:"last_error,omitempty"`
}

// GetPlatformHealth retrieves overall platform health
func (c *ObservabilityClient) GetPlatformHealth(ctx context.Context) (*PlatformHealth, error) {
	url := c.baseURL + config.ServicePaths.Health

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Health endpoint may return 503 when unhealthy but still have valid JSON
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var health PlatformHealth
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &health, nil
}

// FlowInfo represents a flow from the flowbuilder API
type FlowInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	DesiredState    string `json:"desired_state"`
	EffectiveState  string `json:"effective_state"`
	RestartRequired *bool  `json:"restart_required"`
}

// FlowsResponse represents the response from GET /flowbuilder/flows
type FlowsResponse struct {
	Flows []FlowInfo `json:"flows"`
}

// GetFlows retrieves all flows from the flowbuilder API
func (c *ObservabilityClient) GetFlows(ctx context.Context) ([]FlowInfo, error) {
	url := c.baseURL + "/flowbuilder/flows"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var flowsResp FlowsResponse
	if err := json.NewDecoder(resp.Body).Decode(&flowsResp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return flowsResp.Flows, nil
}

// GetComponents retrieves information about all managed components
func (c *ObservabilityClient) GetComponents(ctx context.Context) ([]ComponentInfo, error) {
	url := c.baseURL + config.ComponentPaths.List

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var components []ComponentInfo
	if err := json.NewDecoder(resp.Body).Decode(&components); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return components, nil
}

// WaitForComponentHealthy waits until a specific component reports healthy status.
// This is useful after Docker compose --wait passes (which only checks /health endpoint)
// but before individual components like graph processor have finished initialization.
func (c *ObservabilityClient) WaitForComponentHealthy(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	var lastState string

	for time.Now().Before(deadline) {
		components, err := c.GetComponents(ctx)
		if err != nil {
			lastErr = err
		} else {
			for _, comp := range components {
				if comp.Name == name {
					lastState = comp.State
					if comp.Healthy {
						return nil
					}
					break
				}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	if lastErr != nil {
		return fmt.Errorf("component %s not healthy after %v: last error: %w", name, timeout, lastErr)
	}
	return fmt.Errorf("component %s not healthy after %v: last state: %s", name, timeout, lastState)
}

// WaitForAllComponentsHealthy waits until all components report healthy status.
func (c *ObservabilityClient) WaitForAllComponentsHealthy(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	var unhealthyComponents []string

	for time.Now().Before(deadline) {
		components, err := c.GetComponents(ctx)
		if err != nil {
			lastErr = err
		} else {
			unhealthyComponents = nil
			allHealthy := true
			for _, comp := range components {
				if !comp.Healthy {
					allHealthy = false
					unhealthyComponents = append(unhealthyComponents, fmt.Sprintf("%s(%s)", comp.Name, comp.State))
				}
			}
			if allHealthy {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}

	if lastErr != nil {
		return fmt.Errorf("components not healthy after %v: last error: %w", timeout, lastErr)
	}
	return fmt.Errorf("components not healthy after %v: unhealthy: %v", timeout, unhealthyComponents)
}

// CountFileOutputLines counts lines in file output inside a container using docker exec.
// The containerName should match the container running the file output component.
// The pattern is the file glob pattern (e.g., "/tmp/streamkit-test*.jsonl").
// Returns 0 if files don't exist (not an error - just means no output yet).
func (c *ObservabilityClient) CountFileOutputLines(
	ctx context.Context,
	containerName string,
	pattern string,
) (int, error) {
	// Use docker exec to count lines in the file(s)
	// Shell is needed for glob expansion
	cmd := exec.CommandContext(ctx, "docker", "exec", containerName,
		"sh", "-c", fmt.Sprintf("cat %s 2>/dev/null | wc -l", pattern))

	output, err := cmd.Output()
	if err != nil {
		// If the command fails (e.g., no files match), return 0
		// This is not an error - just means no output files yet
		return 0, nil
	}

	// Parse the line count from output
	countStr := strings.TrimSpace(string(output))
	if countStr == "" {
		return 0, nil
	}

	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, fmt.Errorf("parsing line count %q: %w", countStr, err)
	}

	return count, nil
}

// GetFileOutputLines retrieves the actual content lines from file output inside a container.
// Returns the lines as a slice of strings for content validation.
func (c *ObservabilityClient) GetFileOutputLines(
	ctx context.Context,
	containerName string,
	pattern string,
	maxLines int,
) ([]string, error) {
	// Use docker exec to read lines from the file(s)
	// Shell is needed for glob expansion
	cmdStr := fmt.Sprintf("cat %s 2>/dev/null", pattern)
	if maxLines > 0 {
		cmdStr = fmt.Sprintf("cat %s 2>/dev/null | head -n %d", pattern, maxLines)
	}

	cmd := exec.CommandContext(ctx, "docker", "exec", containerName, "sh", "-c", cmdStr)

	output, err := cmd.Output()
	if err != nil {
		return nil, nil // No files match - return empty slice
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil // Empty output
	}

	return lines, nil
}

// FlowValidation represents the result of flowgraph validation from /components/validate
type FlowValidation struct {
	Timestamp           string                   `json:"timestamp"`
	ValidationStatus    string                   `json:"validation_status"`
	ConnectedComponents [][]string               `json:"connected_components"`
	ConnectedEdges      []map[string]interface{} `json:"connected_edges"`
	DisconnectedNodes   []DisconnectedNode       `json:"disconnected_nodes"`
	OrphanedPorts       []OrphanedPort           `json:"orphaned_ports"`
	StreamWarnings      []StreamWarning          `json:"stream_warnings"`
	Summary             FlowValidationSummary    `json:"summary"`
}

// DisconnectedNode represents a component with no connections
type DisconnectedNode struct {
	ComponentName string   `json:"component_name"`
	Issue         string   `json:"issue"`
	Suggestions   []string `json:"suggestions,omitempty"`
}

// OrphanedPort represents a port with no connections
type OrphanedPort struct {
	ComponentName string `json:"component_name"`
	PortName      string `json:"port_name"`
	Direction     string `json:"direction"`
	ConnectionID  string `json:"connection_id"`
	Pattern       string `json:"pattern"`
	Issue         string `json:"issue"`
	Required      bool   `json:"required"`
}

// StreamWarning represents a JetStream subscriber connected to NATS publisher issue
type StreamWarning struct {
	Severity       string   `json:"severity"`
	SubscriberComp string   `json:"subscriber_component"`
	SubscriberPort string   `json:"subscriber_port"`
	Subjects       []string `json:"subjects"`
	PublisherComps []string `json:"publisher_components"`
	Issue          string   `json:"issue"`
}

// FlowValidationSummary contains summary statistics from flow validation
type FlowValidationSummary struct {
	TotalComponents       int  `json:"total_components"`
	TotalConnections      int  `json:"total_connections"`
	ComponentGroups       int  `json:"component_groups"`
	OrphanedPortCount     int  `json:"orphaned_port_count"`
	DisconnectedNodeCount int  `json:"disconnected_node_count"`
	StreamWarningCount    int  `json:"stream_warning_count"`
	HasStreamIssues       bool `json:"has_stream_issues"`
}

// ValidateFlowGraph calls /components/validate and returns the flow validation result.
// This performs pre-flight validation to catch configuration issues before running tests.
func (c *ObservabilityClient) ValidateFlowGraph(ctx context.Context) (*FlowValidation, error) {
	url := c.baseURL + "/components/validate"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("flow validation request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flow validation returned status %d", resp.StatusCode)
	}

	var validation FlowValidation
	if err := json.NewDecoder(resp.Body).Decode(&validation); err != nil {
		return nil, fmt.Errorf("failed to decode validation response: %w", err)
	}

	return &validation, nil
}

// CheckFlowHealth performs flow validation and returns an error if there are critical issues.
// This is a convenience method for pre-flight checks in e2e test setup.
func (c *ObservabilityClient) CheckFlowHealth(ctx context.Context) error {
	validation, err := c.ValidateFlowGraph(ctx)
	if err != nil {
		return fmt.Errorf("flow validation failed: %w", err)
	}

	// Check for critical stream issues (highest priority)
	// These indicate JetStream subscribers waiting for streams that won't be created
	if len(validation.StreamWarnings) > 0 {
		var issues []string
		for _, w := range validation.StreamWarnings {
			issues = append(issues, w.Issue)
		}
		return fmt.Errorf("critical stream configuration issues: %v", issues)
	}

	// Check for disconnected nodes, but ignore expected gateway components
	// Gateway components (graphql, mcp) query via request/response, not streams
	var criticalDisconnected []string
	for _, n := range validation.DisconnectedNodes {
		// Skip gateway components - they're expected to be disconnected from stream flow
		if isExpectedDisconnectedComponent(n.ComponentName) {
			continue
		}
		criticalDisconnected = append(criticalDisconnected, fmt.Sprintf("%s: %s", n.ComponentName, n.Issue))
	}
	if len(criticalDisconnected) > 0 {
		return fmt.Errorf("disconnected components detected: %v", criticalDisconnected)
	}

	// Check validation status (but only if we have stream issues)
	// "warnings" status from orphaned ports or disconnected gateways is acceptable
	if validation.ValidationStatus == "critical" && validation.Summary.HasStreamIssues {
		return fmt.Errorf("flow validation status is critical")
	}

	return nil
}

// isExpectedDisconnectedComponent returns true for components that are expected
// to not have stream connections (e.g., gateways and coordinators that use request/response patterns)
func isExpectedDisconnectedComponent(name string) bool {
	// HTTP gateway components query via NATS request/response, not stream subscriptions
	// They appear "disconnected" in the flow graph but this is expected behavior
	// Note: GraphQL/MCP gateways are now output ports of graph-processor, not standalone components
	if len(name) > 8 && name[len(name)-8:] == "-gateway" {
		return true
	}
	// Query coordinator uses request/reply to other components, not stream subscriptions
	if name == "graph-query" {
		return true
	}
	return false
}
