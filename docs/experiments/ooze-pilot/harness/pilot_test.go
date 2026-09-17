package pilot

import (
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"testing"

	"github.com/gtramontina/ooze"
	"github.com/gtramontina/ooze/viruses"
	"github.com/gtramontina/ooze/viruses/comparison"
)

// The custom modes qualify runner behavior; only comparison is used for discovery.
type calibration struct{ mode string }

func (v calibration) Incubate(node ast.Node, info *types.Info) []*viruses.Infection {
	expression, ok := node.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	if v.mode == "boundary" {
		name, ok := expression.Y.(*ast.Ident)
		if !ok || name.Name != "MaxEntityIDBytes" || expression.Op != token.GTR {
			return nil
		}
		return comparison.New().Incubate(node, info)
	}
	original := expression.Op
	replacement := token.ADD
	if v.mode == "equivalent" {
		replacement = token.GTR
	}
	return []*viruses.Infection{viruses.NewInfection("Calibration-"+v.mode,
		func() { expression.Op = replacement }, func() { expression.Op = original })}
}

func TestOoze(t *testing.T) {
	subject := os.Getenv("PILOT_SUBJECT")
	if subject == "" {
		t.Skip("run via ../run.py using disposable subjects")
	}
	var operator viruses.Virus = comparison.New()
	mode := os.Getenv("PILOT_OPERATOR")
	if mode != "comparison" {
		operator = calibration{mode: mode}
	}
	ooze.Release(t, ooze.WithRepositoryRoot(subject),
		ooze.WithTestCommand(os.Getenv("PILOT_COMMAND")),
		ooze.IgnoreSourceFiles(os.Getenv("PILOT_IGNORE")),
		ooze.WithViruses(operator), ooze.WithMinimumThreshold(0))
}
