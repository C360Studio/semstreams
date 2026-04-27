package agentictools_test

import (
	"context"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// myExecutor is a minimal tool implementation used in the example.
type myExecutor struct{}

func (myExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        "my_tool",
		Description: "example tool",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

func (myExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "executed"}, nil
}

// ExampleExecutorRegistry shows how a downstream binary registers a custom
// tool executor against the shared tool registry. The binary builds the
// registry once at boot, registers builtins via executors.RegisterBuiltins
// (omitted for brevity), then layers any custom tools on top, and plumbs
// the registry into component.Dependencies.ToolRegistry.
func ExampleExecutorRegistry() {
	reg := agentictools.NewExecutorRegistry()

	// Register a custom tool. Duplicate names error — register once.
	if err := reg.RegisterTool("my_tool", myExecutor{}); err != nil {
		panic(err)
	}

	// In production, the registry is plumbed via component.Dependencies:
	//
	//   deps := component.Dependencies{ToolRegistry: reg}
	//
	// For this example we just call Execute directly.
	res, err := reg.Execute(context.Background(), agentic.ToolCall{
		ID:   "call-1",
		Name: "my_tool",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Content)
	// Output: executed
}
