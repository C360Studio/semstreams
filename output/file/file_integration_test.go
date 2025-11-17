package file_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/output/file"
)

// Package-level shared test client to avoid Docker resource exhaustion
var (
	sharedTestClient *natsclient.TestClient
	sharedNATSClient *natsclient.Client
)

// TestMain sets up a single shared NATS container for all file output tests
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION_TESTS") != "" {
		// Create a single shared test client for integration tests
		testClient, err := natsclient.NewSharedTestClient(
			natsclient.WithJetStream(),
			natsclient.WithKV(),
			natsclient.WithTestTimeout(5*time.Second),
			natsclient.WithStartTimeout(30*time.Second),
		)
		if err != nil {
			panic("Failed to create shared test client: " + err.Error())
		}

		sharedTestClient = testClient
		sharedNATSClient = testClient.Client
	}

	// Run all tests
	exitCode := m.Run()

	// Cleanup integration test resources if they were created
	if sharedTestClient != nil {
		sharedTestClient.Terminate()
	}

	os.Exit(exitCode)
}

// getSharedNATSClient returns the shared NATS client for integration tests
func getSharedNATSClient(t *testing.T) *natsclient.Client {
	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=1 to run.")
	}
	if sharedNATSClient == nil {
		t.Fatal("Shared NATS client not initialized - TestMain should have created it")
	}
	return sharedNATSClient
}

// TestIntegration_BasicJSONLFileWrite tests NATS message triggering file write in JSONL format
func TestIntegration_BasicJSONLFileWrite(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	// Create temporary directory for test
	tmpDir := t.TempDir()

	// Create file output config
	config := file.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.file.jsonl", Required: true},
			},
		},
		Directory:  tmpDir,
		FilePrefix: "test-output",
		Format:     "jsonl",
		Append:     false,
		BufferSize: 10,
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	// Create file output
	fileComp, err := file.NewOutput(rawConfig, deps)
	require.NoError(t, err)
	require.NotNil(t, fileComp)

	fileOutput, ok := fileComp.(component.LifecycleComponent)
	require.True(t, ok)

	// Initialize
	err = fileOutput.Initialize()
	require.NoError(t, err)

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start output
	err = fileOutput.Start(ctx)
	require.NoError(t, err)
	defer fileOutput.Stop(5 * time.Second)

	// Give output time to subscribe
	time.Sleep(100 * time.Millisecond)

	// Publish test messages
	testMessages := []map[string]any{
		{"id": 1, "data": "first message"},
		{"id": 2, "data": "second message"},
		{"id": 3, "data": "third message"},
	}

	for _, msg := range testMessages {
		data, err := json.Marshal(msg)
		require.NoError(t, err)

		err = natsClient.Publish(ctx, "test.file.jsonl", data)
		require.NoError(t, err)
	}

	// Wait for buffer flush (periodic flush is 1 second)
	time.Sleep(2 * time.Second)

	// Read and verify file contents
	outputFile := filepath.Join(tmpDir, "test-output.jsonl")
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 3, "Should have 3 lines in JSONL file")

	// Verify each line is valid JSON
	for i, line := range lines {
		var msg map[string]any
		err := json.Unmarshal([]byte(line), &msg)
		require.NoError(t, err, "Line %d should be valid JSON", i)
		assert.Equal(t, float64(i+1), msg["id"])
	}
}

// TestIntegration_JSONFormatPrettyPrint tests JSON format with pretty printing
func TestIntegration_JSONFormatPrettyPrint(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	tmpDir := t.TempDir()

	config := file.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.file.json", Required: true},
			},
		},
		Directory:  tmpDir,
		FilePrefix: "pretty",
		Format:     "json",
		Append:     false,
		BufferSize: 5,
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	fileComp, err := file.NewOutput(rawConfig, deps)
	require.NoError(t, err)

	fileOutput, ok := fileComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = fileOutput.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = fileOutput.Start(ctx)
	require.NoError(t, err)
	defer fileOutput.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Publish test message with nested structure
	testMsg := map[string]any{
		"id":   1,
		"name": "test",
		"metadata": map[string]any{
			"timestamp": "2024-01-01T00:00:00Z",
			"tags":      []string{"tag1", "tag2"},
		},
	}

	data, err := json.Marshal(testMsg)
	require.NoError(t, err)

	err = natsClient.Publish(ctx, "test.file.json", data)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Read and verify pretty-printed JSON
	outputFile := filepath.Join(tmpDir, "pretty.json")
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	// Verify it's pretty-printed (has indentation)
	contentStr := string(content)
	assert.Contains(t, contentStr, "  ", "Should have indentation")
	assert.Contains(t, contentStr, "\"id\": 1")
	assert.Contains(t, contentStr, "\"metadata\":")
}

// TestIntegration_RawFormat tests raw format (no JSON processing)
func TestIntegration_RawFormat(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	tmpDir := t.TempDir()

	config := file.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.file.raw", Required: true},
			},
		},
		Directory:  tmpDir,
		FilePrefix: "raw",
		Format:     "raw",
		Append:     false,
		BufferSize: 5,
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	fileComp, err := file.NewOutput(rawConfig, deps)
	require.NoError(t, err)

	fileOutput, ok := fileComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = fileOutput.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = fileOutput.Start(ctx)
	require.NoError(t, err)
	defer fileOutput.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Publish raw data (not JSON)
	rawData := []byte("raw message 1")
	err = natsClient.Publish(ctx, "test.file.raw", rawData)
	require.NoError(t, err)

	rawData2 := []byte("raw message 2")
	err = natsClient.Publish(ctx, "test.file.raw", rawData2)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Read and verify raw contents
	outputFile := filepath.Join(tmpDir, "raw.raw")
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	// Raw format doesn't add newlines, messages are concatenated
	assert.Equal(t, "raw message 1raw message 2", string(content))
}

// TestIntegration_AppendMode tests append mode vs truncate mode
func TestIntegration_AppendMode(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "append-test.jsonl")

	// First run: Write initial data with append=false (truncate)
	config1 := file.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.file.append1", Required: true},
			},
		},
		Directory:  tmpDir,
		FilePrefix: "append-test",
		Format:     "jsonl",
		Append:     false, // Truncate mode
		BufferSize: 5,
	}

	rawConfig1, err := json.Marshal(config1)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	fileComp1, err := file.NewOutput(rawConfig1, deps)
	require.NoError(t, err)

	fileOutput1, ok := fileComp1.(component.LifecycleComponent)
	require.True(t, ok)

	err = fileOutput1.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = fileOutput1.Start(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Write first message
	msg1 := map[string]any{"id": 1, "data": "first"}
	data1, _ := json.Marshal(msg1)
	natsClient.Publish(ctx, "test.file.append1", data1)

	time.Sleep(2 * time.Second)
	fileOutput1.Stop(5 * time.Second)

	// Verify first file has 1 line
	content1, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	lines1 := strings.Split(strings.TrimSpace(string(content1)), "\n")
	assert.Len(t, lines1, 1)

	// Second run: Append more data with append=true
	config2 := file.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.file.append2", Required: true},
			},
		},
		Directory:  tmpDir,
		FilePrefix: "append-test",
		Format:     "jsonl",
		Append:     true, // Append mode
		BufferSize: 5,
	}

	rawConfig2, err := json.Marshal(config2)
	require.NoError(t, err)

	fileComp2, err := file.NewOutput(rawConfig2, deps)
	require.NoError(t, err)

	fileOutput2, ok := fileComp2.(component.LifecycleComponent)
	require.True(t, ok)

	err = fileOutput2.Initialize()
	require.NoError(t, err)

	err = fileOutput2.Start(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Write second message
	msg2 := map[string]any{"id": 2, "data": "second"}
	data2, _ := json.Marshal(msg2)
	natsClient.Publish(ctx, "test.file.append2", data2)

	time.Sleep(2 * time.Second)
	fileOutput2.Stop(5 * time.Second)

	// Verify file now has 2 lines (appended)
	content2, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	lines2 := strings.Split(strings.TrimSpace(string(content2)), "\n")
	assert.Len(t, lines2, 2, "Append mode should preserve existing content")
}

// TestIntegration_BufferFlushing tests buffer flushing when buffer is full
func TestIntegration_BufferFlushing(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	tmpDir := t.TempDir()

	// Small buffer size to test flushing
	config := file.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input", Type: "nats", Subject: "test.file.buffer", Required: true},
			},
		},
		Directory:  tmpDir,
		FilePrefix: "buffer-test",
		Format:     "jsonl",
		Append:     false,
		BufferSize: 3, // Flush after 3 messages
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	fileComp, err := file.NewOutput(rawConfig, deps)
	require.NoError(t, err)

	fileOutput, ok := fileComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = fileOutput.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = fileOutput.Start(ctx)
	require.NoError(t, err)
	defer fileOutput.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Publish exactly buffer size messages
	for i := 1; i <= 3; i++ {
		msg := map[string]any{"id": i, "data": "message"}
		data, _ := json.Marshal(msg)
		natsClient.Publish(ctx, "test.file.buffer", data)
	}

	// Give a short time for immediate flush (buffer full)
	time.Sleep(500 * time.Millisecond)

	// Verify file has messages (buffer was flushed when full)
	outputFile := filepath.Join(tmpDir, "buffer-test.jsonl")
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	assert.Len(t, lines, 3, "Buffer should flush when full")
}

// TestIntegration_MultipleSubjects tests file output with multiple input subjects
func TestIntegration_MultipleSubjects(t *testing.T) {
	natsClient := getSharedNATSClient(t)

	tmpDir := t.TempDir()

	config := file.Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{Name: "input1", Type: "nats", Subject: "test.file.multi.1", Required: true},
				{Name: "input2", Type: "nats", Subject: "test.file.multi.2", Required: true},
			},
		},
		Directory:  tmpDir,
		FilePrefix: "multi",
		Format:     "jsonl",
		Append:     false,
		BufferSize: 10,
	}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	fileComp, err := file.NewOutput(rawConfig, deps)
	require.NoError(t, err)

	fileOutput, ok := fileComp.(component.LifecycleComponent)
	require.True(t, ok)

	err = fileOutput.Initialize()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = fileOutput.Start(ctx)
	require.NoError(t, err)
	defer fileOutput.Stop(5 * time.Second)

	time.Sleep(100 * time.Millisecond)

	// Publish to both subjects
	msg1 := map[string]any{"source": "subject1", "data": "from 1"}
	data1, _ := json.Marshal(msg1)
	natsClient.Publish(ctx, "test.file.multi.1", data1)

	msg2 := map[string]any{"source": "subject2", "data": "from 2"}
	data2, _ := json.Marshal(msg2)
	natsClient.Publish(ctx, "test.file.multi.2", data2)

	time.Sleep(2 * time.Second)

	// Verify file has messages from both subjects
	outputFile := filepath.Join(tmpDir, "multi.jsonl")
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 2, "Should have messages from both subjects")

	// Parse and verify sources
	sources := make([]string, 0)
	for _, line := range lines {
		var msg map[string]any
		json.Unmarshal([]byte(line), &msg)
		sources = append(sources, msg["source"].(string))
	}

	assert.Contains(t, sources, "subject1")
	assert.Contains(t, sources, "subject2")
}
