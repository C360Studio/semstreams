// Package llm provides LLM client abstractions for OpenAI-compatible APIs.
//
// This package enables semstreams to integrate with any OpenAI-compatible
// LLM service for:
//   - Community summarization (clustering package)
//   - Search answer generation (querymanager package)
//   - General inference tasks
//
// # Supported Backends
//
// The package uses the OpenAI SDK, so it works with any compatible backend:
//   - seminstruct (recommended for local llama.cpp inference)
//   - OpenAI cloud
//   - Ollama
//   - vLLM
//   - Any OpenAI-compatible API
//
// # Usage
//
// Create a client:
//
//	cfg := llm.OpenAIConfig{
//	    BaseURL: "http://seminstruct:8083/v1",
//	    Model:   "qwen3-0.6b",
//	}
//	client, err := llm.NewOpenAIClient(cfg)
//
// Make a chat completion request:
//
//	resp, err := client.ChatCompletion(ctx, llm.ChatRequest{
//	    SystemPrompt: "You are a helpful assistant.",
//	    UserPrompt:   "Summarize this community...",
//	    MaxTokens:    150,
//	})
//
// # Prompts
//
// The package provides prompt templates as package variables that can be
// used directly or overridden via JSON file:
//
//	// Use built-in prompt
//	rendered, err := llm.CommunityPrompt.Render(data)
//
//	// Override via file
//	llm.LoadPromptsFromFile("prompts.json")
//
// Prompts understand the 6-part federated entity ID notation:
//
//	{org}.{platform}.{domain}.{system}.{type}.{instance}
//
// # Configuration
//
// LLM configuration is part of the graph processor config:
//
//	{
//	    "llm": {
//	        "provider": "openai",
//	        "base_url": "http://seminstruct:8083/v1",
//	        "model": "qwen3-0.6b"
//	    }
//	}
package llm
