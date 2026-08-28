package entityidaudit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"

	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// Segment-semantics rules (ADR-102; entity-id-contract requirement "Segment
// semantics are enforced by the entity-ID corpus audit"). They run over
// lexically valid candidates only — a lexical finding already reports the
// candidate — and cover two corpora:
//
//   - production Go: every .go file that is not a _test.go, not beneath a
//     testdata directory, and not beneath the repository's test/ tree;
//   - configs: JSON and YAML beneath a configs/ directory.
//
// Test fixtures stay lexical-only: they legitimately carry literal authorities
// and unregistered taxonomies.
const (
	// ReasonAuthorityLiteral reports a literal, non-wildcard, non-template value
	// in positions 1-2 (org.platform) of a production builder, declaration
	// pattern, or prefix constant — including a whole six-segment literal minted
	// on one of the mintingSurfaces. The authority is deps.Platform, never a literal.
	ReasonAuthorityLiteral = "authority_literal"
	// ReasonDomainUnregistered reports a literal position-4 (domain) value in
	// production Go that is neither framework-reserved nor a registered
	// EntityDomainDelegation.
	ReasonDomainUnregistered = "domain_unregistered"
	// ReasonInstanceReserved reports a production instance value equal to a
	// hierarchy-container padding token (group, container, level).
	ReasonInstanceReserved = "instance_reserved"
)

var (
	formatVerbRE    = regexp.MustCompile(`^%[sdvqxX]$`)
	segmentLiteral  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	entityNameToken = "entity"
)

// segmentFinding applies the segment rules to one lexically valid candidate and
// returns the first applicable reason, or "".
func segmentFinding(candidate Candidate, registered map[string]bool) string {
	tokens := strings.Split(strings.TrimSuffix(candidate.Value, "."), ".")
	production := isProductionGo(candidate.File)
	config := isConfigPath(candidate.File)
	switch candidate.Language {
	case LanguageDeclarationPattern, LanguageFormatBuilder, LanguagePrefixConstant:
		if (production || config) && authorityIsLiteral(tokens) {
			return ReasonAuthorityLiteral
		}
	case LanguageLiteral:
		// A whole six-segment literal this code mints for its OWN entity is a
		// builder too — the most literal one there is. Scoped to the minting
		// surfaces: a triple subject, a typed reference, or a state ID may name
		// another deployment's entity, and naming a foreign authority there is
		// legitimate.
		if production && isMintingSurface(candidate.Surface) && authorityIsLiteral(tokens) {
			return ReasonAuthorityLiteral
		}
	}
	if !production {
		return ""
	}
	switch candidate.Language {
	case LanguageLiteral, LanguageDeclarationPattern, LanguageFormatBuilder, LanguagePrefixConstant:
		if len(tokens) >= 4 && isLiteralPosition(tokens[3]) &&
			!semtypes.IsFrameworkEntityDomain(tokens[3]) && !registered[tokens[3]] {
			return ReasonDomainUnregistered
		}
		if len(tokens) == 6 && isLiteralPosition(tokens[5]) && semtypes.IsReservedInstanceToken(tokens[5]) {
			return ReasonInstanceReserved
		}
	}
	return ""
}

// mintingSurfaces is the closed set of surfaces on which production Go states
// the identity of the entity it is ITSELF minting, rather than referring to an
// entity someone else minted. Only these are judged for a literal authority.
var mintingSurfaces = [...]string{"go-constructor:EntityID", "go-return:EntityID"}

// isMintingSurface reports whether surface is one this code mints its own
// identity on.
func isMintingSurface(surface string) bool {
	for _, minting := range mintingSurfaces {
		if surface == minting {
			return true
		}
	}
	return false
}

// authorityIsLiteral reports whether either authority position (1-2) is a
// fixed literal rather than a wildcard, format verb, or substitution template.
func authorityIsLiteral(tokens []string) bool {
	return len(tokens) >= 2 && (isLiteralPosition(tokens[0]) || isLiteralPosition(tokens[1]))
}

// isLiteralPosition reports whether a position token is a fixed literal rather
// than a wildcard, a format verb, or a substitution template.
func isLiteralPosition(token string) bool {
	if token == "*" || formatVerbRE.MatchString(token) || strings.HasPrefix(token, "$") {
		return false
	}
	return segmentLiteral.MatchString(token)
}

func isProductionGo(path string) bool {
	clean := filepath.ToSlash(path)
	if !strings.HasSuffix(clean, ".go") || strings.HasSuffix(clean, "_test.go") {
		return false
	}
	slashed := "/" + clean + "/"
	return !strings.Contains(slashed, "/testdata/") && !strings.Contains(slashed, "/test/")
}

func isConfigPath(path string) bool {
	clean := filepath.ToSlash(path)
	switch strings.ToLower(filepath.Ext(clean)) {
	case ".json", ".json5", ".yaml", ".yml":
	default:
		return false
	}
	return strings.Contains("/"+clean+"/", "/configs/")
}

// formatBuilderTokens splits a fmt.Sprintf format string into position tokens
// when every token is a format verb or a canonical literal segment and the
// shape is one an entity-ID builder takes: exactly six positions, or two to
// five positions whose first two are template positions (the org.platform
// authority template of a prefix builder). Returns nil otherwise.
func formatBuilderTokens(format string) []string {
	tokens := strings.Split(format, ".")
	if len(tokens) < 2 || len(tokens) > 6 {
		return nil
	}
	for _, token := range tokens {
		if !formatVerbRE.MatchString(token) && !segmentLiteral.MatchString(token) {
			return nil
		}
	}
	if len(tokens) == 6 || (formatVerbRE.MatchString(tokens[0]) && formatVerbRE.MatchString(tokens[1])) {
		return tokens
	}
	return nil
}

// validateFormatBuilder is the lexical rule for a format-builder candidate:
// every literal token must be a canonical segment (verbs are template positions).
func validateFormatBuilder(value string) error {
	for _, token := range strings.Split(value, ".") {
		if formatVerbRE.MatchString(token) {
			continue
		}
		if err := semtypes.ValidateEntityIDPrefix(token); err != nil {
			return err
		}
	}
	return nil
}

// validatePrefixConstant is the lexical rule for a trailing-dot prefix
// constant: the tokens before the trailing dot form a query prefix.
func validatePrefixConstant(value string) error {
	return semtypes.ValidateEntityIDPrefix(strings.TrimSuffix(value, "."))
}

// isDottedPrefixConstant reports whether a declared string constant is a
// trailing-dot dotted prefix an entity-ID builder concatenates onto: two or more
// dotted tokens ending in ".", declared under a name that says "entity".
func isDottedPrefixConstant(name, value string) bool {
	if !strings.Contains(strings.ToLower(name), entityNameToken) || !strings.HasSuffix(value, ".") {
		return false
	}
	return strings.Count(value, ".") >= 2
}

// collectRegisteredDomains gathers every entity domain declared as an
// EntityDomainDelegation composite literal in production Go: the registered set
// the domain_unregistered rule consults, beside the framework-reserved set.
func collectRegisteredDomains(files []string, symbols *goSymbols) (map[string]bool, error) {
	registered := make(map[string]bool)
	for _, path := range files {
		if !isProductionGo(path) {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		resolve := func(expr ast.Expr) (string, bool) {
			return resolveStaticString(expr, symbols.byFile[path], symbols.byPackage[file.Name.Name], nil)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			switch typed := literal.Type.(type) {
			case *ast.ArrayType:
				// []EntityDomainDelegation{{...}, {...}}: the elements elide their type.
				if expressionName(typed.Elt) != "EntityDomainDelegation" {
					return true
				}
				for _, element := range literal.Elts {
					if inner, ok := element.(*ast.CompositeLit); ok {
						collectDelegationDomain(inner, registered, resolve)
					}
				}
				return false
			default:
				if expressionName(literal.Type) == "EntityDomainDelegation" {
					collectDelegationDomain(literal, registered, resolve)
				}
			}
			return true
		})
	}
	return registered, nil
}

// collectDelegationDomain records the static Domain of one delegation literal.
func collectDelegationDomain(literal *ast.CompositeLit, registered map[string]bool, resolve func(ast.Expr) (string, bool)) {
	for _, element := range literal.Elts {
		keyed, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := keyed.Key.(*ast.Ident); ok && key.Name == "Domain" {
			if value, ok := resolve(keyed.Value); ok {
				registered[value] = true
			}
		}
	}
}
