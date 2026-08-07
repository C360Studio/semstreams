// Command port-grammar-control manages the frozen Foundation B migration plan.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/c360studio/semstreams/internal/portgrammarcontrol"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "check", "check, dry-run, or rewrite")
	root := flag.String("root", ".", "repository root")
	output := flag.String("out", "", "caller-selected output root for rewrite/check")
	applyDispositions := flag.Bool("apply-dispositions", false, "apply the 74 reviewed dispositions")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	switch *mode {
	case "check":
		plan, err := portgrammarcontrol.LoadPlan(absRoot)
		if err != nil {
			return err
		}
		if *output != "" {
			outputs, err := portgrammarcontrol.Rewrite(absRoot, *output, plan, portgrammarcontrol.RewriteOptions{Check: true, ApplyDispositions: *applyDispositions})
			if err != nil {
				return err
			}
			fmt.Print(string(portgrammarcontrol.MarshalOutputs(outputs)))
			return nil
		}
		population, err := portgrammarcontrol.Census(absRoot)
		if err != nil {
			return err
		}
		if err := plan.ValidateAgainst(population); err != nil {
			return err
		}
		fmt.Printf("population complete: configs=%d documents=%d mechanical=%d dispositions=%d go=%d files=%d sources=%d\n",
			len(plan.ConfigItems()), plan.ConfigDocumentCount(), plan.MechanicalCount(), len(plan.Dispositions),
			len(plan.GoItems()), plan.GoFileCount(), plan.GoSourceCount())
		return nil
	case "dry-run":
		plan, err := portgrammarcontrol.LoadPlan(absRoot)
		if err != nil {
			return err
		}
		outputs, err := portgrammarcontrol.Rewrite(absRoot, "", plan, portgrammarcontrol.RewriteOptions{DryRun: true, ApplyDispositions: *applyDispositions})
		if err != nil {
			return err
		}
		fmt.Print(string(portgrammarcontrol.MarshalOutputs(outputs)))
		return nil
	case "rewrite":
		if *output == "" {
			return fmt.Errorf("-out is required for rewrite")
		}
		plan, err := portgrammarcontrol.LoadPlan(absRoot)
		if err != nil {
			return err
		}
		outputs, err := portgrammarcontrol.Rewrite(absRoot, *output, plan, portgrammarcontrol.RewriteOptions{ApplyDispositions: *applyDispositions})
		if err != nil {
			return err
		}
		fmt.Print(string(portgrammarcontrol.MarshalOutputs(outputs)))
		return nil
	default:
		return fmt.Errorf("unknown mode %q", *mode)
	}
}
