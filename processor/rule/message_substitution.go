// Package rule - Inbound-message substitution context.
//
// Tool-call governance rules (ADR-039) need to template verdict subjects
// and audit reasons with values pulled from the proposed tool-call
// payload: `agent.toolcall.rejected.$message.execution_id`,
// `Reason: "bash command $message.tool_args.command rejected for $message.tool_name"`.
// The existing `$entity.*` / `$related.*` / `$state.*` / `$caller.*` /
// `$schedule.*` namespaces all read from entity-side context; none of
// them carry the payload of the message that triggered evaluation.
//
// `$message.<field_path>` closes that gap. Resolution walks a
// `map[string]any` along the dotted path — same shape the expression
// evaluator already uses for condition matching (see
// `ExpressionRule.extractNestedValue` in `expression_factory.go`). Deep
// paths (`$message.tool_args.command`) are supported per the
// `$entity.triple.X` precedent.
//
// # Behaviour
//
// A nil or empty data map is a no-op — every `$message.*` token in the
// template survives substitution and trips the unresolvedTemplateVarRe
// warning. Same for paths that descend through a non-map intermediate or
// hit a missing terminal key. The literal stays in the output so authors
// see the silent-pass loudly, instead of empty strings landing in
// downstream subjects/keys.
//
// # Namespace boundary
//
// `$message.*` shares the substitution-token regex with the other
// namespaces in `execution_context.go`. The pass runs alongside the
// other ApplyXSubstitutions calls in `ExecutionContext.SubstituteVariables`
// and has no overlap with the other token shapes by construction.
package rule

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/c360studio/semstreams/processor/rule/expression"
)

// messageTokenRe matches `$message.<field_path>` where the path is one
// or more dot-separated word segments. The non-capturing `\b` boundary
// at the end stops the match at anything that isn't a word character or
// dot — so a token followed by punctuation (`$message.loop_id.`,
// `$message.call_id,`) doesn't over-consume into the trailing literal.
//
// The greedy-by-default `[\w.]+` is fine here: substitution iterates the
// FindAllStringSubmatchIndex results in order, longest-first per regex
// semantics, so deeper paths get tried before shorter prefixes that
// would otherwise win. Resolution failure leaves the literal in place —
// see the package doc for why that is the correct silent-pass behaviour.
var messageTokenRe = regexp.MustCompile(`\$message\.[\w]+(?:\.[\w]+)*`)

// applyMessageSubstitutions replaces `$message.<field_path>` tokens in
// template against data using dotted-path access into the map. nil/empty
// data leaves every token untouched so the unresolved-template warning
// in execution_context.go fires.
//
// Path-resolution failure (intermediate not a map, terminal key missing)
// is also a no-op — same surfacing behaviour as `$entity.triple.X`
// missing a predicate on the entity at fire time. Authors see the
// literal in the output and the warning, not a silently rendered empty.
func applyMessageSubstitutions(template string, data map[string]any) string {
	if len(data) == 0 {
		return template
	}
	return messageTokenRe.ReplaceAllStringFunc(template, func(token string) string {
		// token shape: "$message.<dotted.path>"
		path := strings.TrimPrefix(token, "$message.")
		if path == "" {
			return token
		}
		value, ok := expression.ExtractMessageValue(expression.MessageFields(data), path)
		if !ok {
			return token
		}
		return fmt.Sprintf("%v", value)
	})
}
