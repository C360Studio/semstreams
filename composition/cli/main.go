// Package cli is the exported entry point through which any binary that
// composes a component registry serves the composition verbs: catalog,
// validate <config>, graph <config> [--mermaid]. A product calls Dispatch from
// its own main before its flag parsing; nothing here touches NATS.
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	"github.com/c360studio/semstreams/config"
)

// Exit codes.
const (
	ExitOK       = 0
	ExitFindings = 1 // validate: the result has error-severity findings
	ExitUsage    = 2 // bad verb, missing path, unreadable configuration
)

// Verbs served by Main.
const (
	VerbCatalog  = "catalog"
	VerbValidate = "validate"
	VerbGraph    = "graph"
)

// IsVerb reports whether arg names a composition verb, so a binary whose
// registry is expensive to build can decide before building it:
//
//	if len(os.Args) > 1 && cli.IsVerb(os.Args[1]) {
//		os.Exit(cli.Main(os.Args[1:], buildRegistry(), os.Stdout, os.Stderr))
//	}
func IsVerb(arg string) bool {
	switch arg {
	case VerbCatalog, VerbValidate, VerbGraph:
		return true
	default:
		return false
	}
}

// Dispatch serves args when args[0] is a composition verb and reports whether
// it did, so a binary with a ready registry can fall through to its own flags
// otherwise:
//
//	if code, ok := cli.Dispatch(os.Args[1:], registry, os.Stdout, os.Stderr); ok {
//		os.Exit(code)
//	}
func Dispatch(args []string, registry *component.Registry, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 || !IsVerb(args[0]) {
		return ExitOK, false
	}
	return Main(args, registry, stdout, stderr), true
}

// Main serves one verb against the given registry and returns the exit code.
// catalog prints every registered factory with its schema and default ports;
// validate <config> prints the composition.Result and exits ExitFindings when
// it has errors; graph <config> [--mermaid] prints the projection as JSON or
// Mermaid.
func Main(args []string, registry *component.Registry, stdout, stderr io.Writer) int {
	if registry == nil {
		fmt.Fprintln(stderr, "composition: no component registry")
		return ExitUsage
	}
	if len(args) == 0 {
		usage(stderr)
		return ExitUsage
	}
	switch args[0] {
	case VerbCatalog:
		return printJSON(stdout, stderr, composition.Catalog(registry))
	case VerbValidate:
		result, code := validateFile(args[1:], registry, stderr)
		if result == nil {
			return code
		}
		if code := printJSON(stdout, stderr, result); code != ExitOK {
			return code
		}
		if len(result.Errors) > 0 {
			return ExitFindings
		}
		return ExitOK
	case VerbGraph:
		mermaid := false
		var rest []string
		for _, arg := range args[1:] {
			if arg == "--mermaid" {
				mermaid = true
				continue
			}
			rest = append(rest, arg)
		}
		result, code := validateFile(rest, registry, stderr)
		if result == nil {
			return code
		}
		if mermaid {
			if _, err := io.WriteString(stdout, composition.Mermaid(result.Graph)); err != nil {
				fmt.Fprintf(stderr, "composition: write output: %v\n", err)
				return ExitUsage
			}
			return ExitOK
		}
		return printJSON(stdout, stderr, result.Graph)
	default:
		usage(stderr)
		return ExitUsage
	}
}

func validateFile(args []string, registry *component.Registry, stderr io.Writer) (*composition.Result, int) {
	if len(args) != 1 {
		usage(stderr)
		return nil, ExitUsage
	}
	cfg, err := config.NewLoader().LoadFile(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "composition: load %s: %v\n", args[0], err)
		return nil, ExitUsage
	}
	result, err := composition.Validate(registry, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "composition: validate: %v\n", err)
		return nil, ExitUsage
	}
	return result, ExitOK
}

func printJSON(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "composition: encode output: %v\n", err)
		return ExitUsage
	}
	return ExitOK
}

func usage(stderr io.Writer) {
	fmt.Fprint(stderr, `composition verbs:
  catalog                       print every registered factory with its schema and default ports
  validate <config-path>        validate the composition; exit 1 on error findings
  graph <config-path> [--mermaid] print the projection as JSON or Mermaid
`)
}
