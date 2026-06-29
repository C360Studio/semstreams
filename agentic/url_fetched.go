package agentic

import (
	"regexp"
	"strings"
)

// bashToolName is the tool name the bash executor registers. URL derivation
// only applies to steps from this tool.
const bashToolName = "bash"

// fetchVerbs are the leading programs that perform an external URL fetch. A URL
// is only counted when it is an argument to one of these — a URL inside an
// `echo` string, a `grep` pattern, or a filesystem path (/var/log/http.log) has
// a non-fetch leading program and is correctly excluded.
var fetchVerbs = map[string]bool{
	"curl":  true,
	"wget":  true,
	"http":  true, // httpie
	"https": true, // httpie
	"fetch": true,
}

// shellSep splits a command line on the unquoted sequencing operators. Single
// `&` (background) and `&&`-internal query-string `&` are deliberately NOT
// separators here; `&&` is matched before single tokens via alternation order.
var shellSep = regexp.MustCompile(`&&|\|\||;|\||\n`)

// urlPattern matches an http(s) URL token, preserving query strings (`?a=1&b=2`)
// and stopping only at whitespace, quotes, or shell redirection characters.
var urlPattern = regexp.MustCompile("https?://[^\\s'\"`<>\\\\]+")

// BashStepURLs returns the external URLs a bash trajectory step fetched, or nil
// for any other tool or when no fetch URL is present. toolArgs is the step's
// ToolArguments map; the bash command lives under the "command" key.
func BashStepURLs(toolName string, toolArgs map[string]any) []string {
	if toolName != bashToolName {
		return nil
	}
	cmd, _ := toolArgs["command"].(string)
	if cmd == "" {
		return nil
	}
	return ExtractFetchedURLs(cmd)
}

// ExtractFetchedURLs derives the external URLs a bash command fetches. It
// inspects only sub-commands whose leading program is a known fetch verb
// (curl/wget/httpie), so URLs that appear as data — echo strings, grep
// patterns, filesystem paths — are excluded. Returns nil when no fetch URL is
// detected (no false-positive noise); results are order-preserving and
// de-duplicated. This is a cheap observability heuristic, not a shell parser:
// it inspects only the leading token, so wrapped/prefixed fetches —
// `sudo curl ...`, `time curl ...`, `HTTPS_PROXY=x curl ...`, `echo url | curl -`
// — are known misses (under-count, never over-count).
func ExtractFetchedURLs(command string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, sub := range shellSep.Split(command, -1) {
		fields := strings.Fields(sub)
		if len(fields) == 0 {
			continue
		}
		if !fetchVerbs[leadingProgram(fields[0])] {
			continue
		}
		for _, u := range urlPattern.FindAllString(sub, -1) {
			u = strings.TrimRight(u, ".,);")
			if u != "" && !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}

// leadingProgram normalizes a sub-command's first token to the bare program
// name: strips a leading path (/usr/bin/curl → curl) and lowercases.
func leadingProgram(tok string) string {
	if i := strings.LastIndex(tok, "/"); i >= 0 {
		tok = tok[i+1:]
	}
	return strings.ToLower(tok)
}
