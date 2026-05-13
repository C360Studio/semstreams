package config

import (
	"os"
	"regexp"
)

// envVarRe matches ${VAR:-default}, ${VAR}, and $VAR patterns.
//
// The bare-`$VAR` arm is intentionally uppercase-only (POSIX env-var
// convention). Lowercase-prefixed `$word` tokens are reserved for the
// rule engine's substitution namespaces (`$message.*`, `$entity.*`,
// `$related.*`, `$state.*`, `$caller.*`, `$schedule.*` — see
// `processor/rule/execution_context.go`). Operators commonly run this
// helper on whole-file JSON configs that embed `inline_rules`; eating
// those tokens at load time silently broke the rule templates and
// produced garbage subjects/properties downstream (the beta.71 bug
// from semspec `e2e-mock.json`). Operators with bare-lowercase env
// refs must migrate to the braced `${var}` form.
var envVarRe = regexp.MustCompile(`\$\{([^}:]+)(:-([^}]*))?\}|\$([A-Z_][A-Z0-9_]*)`)

// ExpandEnvWithDefaults expands environment variables in a string,
// supporting ${VAR:-default} syntax for default values.
//
// Patterns:
//   - ${VAR} - expands to value of VAR, or empty if unset
//   - ${VAR:-default} - expands to value of VAR, or "default" if unset
//   - $VAR - expands to value of VAR (uppercase identifiers only;
//     lowercase prefixes belong to the rule engine's substitution
//     namespaces and pass through unchanged)
func ExpandEnvWithDefaults(s string) string {
	return envVarRe.ReplaceAllStringFunc(s, func(match string) string {
		submatches := envVarRe.FindStringSubmatch(match)

		// $VAR form (group 4)
		if submatches[4] != "" {
			return os.Getenv(submatches[4])
		}

		// ${VAR} or ${VAR:-default} form
		varName := submatches[1]
		value := os.Getenv(varName)

		// If value is set, use it
		if value != "" {
			return value
		}

		// If unset, check for default (group 3)
		if submatches[2] != "" {
			return submatches[3] // The default value (may be empty)
		}

		return ""
	})
}
