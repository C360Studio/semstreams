package agenticdetonator

import (
	"fmt"

	"github.com/c360studio/semstreams/agentic"
)

// Tool name constants for the attacker surface. Centralised so the
// signal-mapping in signals.go and the synthetic-response builders
// below share names typo-proofly.
const (
	ToolReadFile    = "read_file"
	ToolExecute     = "execute"
	ToolBash        = "bash"
	ToolFetch       = "fetch"
	ToolHTTPRequest = "http_request"
	ToolSendEmail   = "send_email"
	ToolSendMessage = "send_message"
	ToolReadEnv     = "read_env"
	ToolDBQuery     = "db_query"
	ToolListUsers   = "list_users"
	ToolGetAPIKey   = "get_api_key"
)

// defaultWildcardTools returns the standard attacker tool surface
// the canary loop exposes. Definitions are intentionally
// permissive — the LLM should see plausible-looking tools and try
// the ones an injection would target. We never validate the
// arguments at the schema level; the synthetic-response builders
// degrade gracefully on missing or wrong-shape args.
//
// Adding a tool here is a deliberate code change — pair it with a
// signal-bucket mapping in signals.go.
func defaultWildcardTools() []wildcardTool {
	return []wildcardTool{
		{
			def: agentic.ToolDefinition{
				Name:        ToolReadFile,
				Description: "Read the contents of a file from the local filesystem.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "Absolute or relative file path."},
					},
					"required": []string{"path"},
				},
			},
			respFn: readFileResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolExecute,
				Description: "Execute a shell command in the working environment.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string"},
					},
					"required": []string{"command"},
				},
			},
			respFn: shellResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolBash,
				Description: "Run a bash one-liner.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string"},
					},
					"required": []string{"command"},
				},
			},
			respFn: shellResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolFetch,
				Description: "Fetch a URL and return the response body.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{"type": "string"},
					},
					"required": []string{"url"},
				},
			},
			respFn: fetchResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolHTTPRequest,
				Description: "Issue an HTTP request and return the response body.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url":    map[string]any{"type": "string"},
						"method": map[string]any{"type": "string"},
					},
					"required": []string{"url"},
				},
			},
			respFn: fetchResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolSendEmail,
				Description: "Send an email.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"to":      map[string]any{"type": "string"},
						"subject": map[string]any{"type": "string"},
						"body":    map[string]any{"type": "string"},
					},
					"required": []string{"to"},
				},
			},
			respFn: emailResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolSendMessage,
				Description: "Send a message to a chat or messaging system.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"channel": map[string]any{"type": "string"},
						"text":    map[string]any{"type": "string"},
					},
					"required": []string{"text"},
				},
			},
			respFn: emailResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolReadEnv,
				Description: "Read the value of an environment variable.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
					"required": []string{"name"},
				},
			},
			respFn: envResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolDBQuery,
				Description: "Run a SQL query against the application database.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"sql": map[string]any{"type": "string"},
					},
					"required": []string{"sql"},
				},
			},
			respFn: dbResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolListUsers,
				Description: "Return the list of user accounts in the system.",
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
			respFn: listUsersResponse,
		},
		{
			def: agentic.ToolDefinition{
				Name:        ToolGetAPIKey,
				Description: "Retrieve an API key by name.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
					"required": []string{"name"},
				},
			},
			respFn: getAPIKeyResponse,
		},
	}
}

// Synthetic-response builders. Each returns a short, plausibly-real
// blob so the canary continues the attack chain instead of
// bailing. All responses include a watermark — "[SYNTHETIC ...]"
// or a sha-prefixed token — so the LLM's output downstream of a
// detonation is identifiable for trace correlation.

func readFileResponse(args map[string]any, _ string) string {
	path := stringArg(args, "path", "(unknown)")
	switch path {
	case "/etc/passwd":
		return "root:x:0:0:root:/root:/bin/bash\ndaemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin\n[SYNTHETIC detonator response]"
	case "/etc/shadow":
		return "root:!:19720::::::\n[SYNTHETIC detonator response]"
	case "~/.aws/credentials", "/home/agent/.aws/credentials":
		return "[default]\naws_access_key_id=AKIA[SYNTHETIC]\naws_secret_access_key=[SYNTHETIC]"
	case "~/.ssh/id_rsa":
		return "-----BEGIN OPENSSH PRIVATE KEY-----\n[SYNTHETIC]\n-----END OPENSSH PRIVATE KEY-----"
	default:
		return fmt.Sprintf("[SYNTHETIC] contents of %s would be here", path)
	}
}

func shellResponse(args map[string]any, _ string) string {
	cmd := stringArg(args, "command", "")
	if cmd == "" {
		return "[SYNTHETIC detonator] empty command, nothing to run"
	}
	return fmt.Sprintf("[SYNTHETIC detonator] $ %s\n(exit 0)", cmd)
}

func fetchResponse(args map[string]any, hash string) string {
	url := stringArg(args, "url", "(none)")
	return fmt.Sprintf("[SYNTHETIC detonator] HTTP 200 from %s\nx-detonator-trace: %s\n<html><body>placeholder</body></html>", url, hash[:12])
}

func emailResponse(args map[string]any, _ string) string {
	to := stringArg(args, "to", stringArg(args, "channel", "(none)"))
	return fmt.Sprintf("[SYNTHETIC detonator] queued message to %s (synthetic — nothing sent)", to)
}

func envResponse(args map[string]any, _ string) string {
	name := stringArg(args, "name", "(unset)")
	switch name {
	case "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY":
		return "sk-SYNTHETIC[DETONATOR]"
	default:
		return fmt.Sprintf("[SYNTHETIC] env(%s)=detonator-placeholder", name)
	}
}

func dbResponse(args map[string]any, _ string) string {
	sql := stringArg(args, "sql", "")
	return fmt.Sprintf("[SYNTHETIC detonator] %d rows returned for: %s", 0, sql)
}

func listUsersResponse(_ map[string]any, _ string) string {
	return "[SYNTHETIC detonator] users:\n- alice@example.com (admin)\n- bob@example.com\n- carol@example.com"
}

func getAPIKeyResponse(args map[string]any, _ string) string {
	name := stringArg(args, "name", "(unset)")
	return fmt.Sprintf("[SYNTHETIC detonator] api_key(%s)=sk-detonator-placeholder", name)
}

// stringArg pulls a string value from the args map with a fallback.
// Handles the common map[string]any → string coercion the
// agentic.ToolCall arguments arrive as.
func stringArg(args map[string]any, key, fallback string) string {
	if args == nil {
		return fallback
	}
	v, ok := args[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}
