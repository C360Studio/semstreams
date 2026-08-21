package contract

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const semstreamsModulePath = "github.com/c360studio/semstreams"

// TestProductionStructsRetainNoContext type-checks the production build and
// prevents runtime authority from being hidden in a struct. Context parameters
// on operation callbacks remain valid; only stored values and provider results
// are rejected.
func TestProductionStructsRetainNoContext(t *testing.T) {
	t.Parallel()

	root := repoRootForComponentStatusRetirement(t)
	loaded, err := packages.Load(&packages.Config{
		Dir: root,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo,
	}, "./...")
	if err != nil {
		t.Fatalf("load production packages: %v", err)
	}
	if errs := packageErrors(loaded); len(errs) > 0 {
		t.Fatalf("type-check production packages:\n  %s", strings.Join(errs, "\n  "))
	}

	contextPkg := importedPackage(loaded, "context")
	if contextPkg == nil {
		t.Fatal("production package graph does not import context")
	}
	contextType := contextPkg.Scope().Lookup("Context").Type()
	cancelType := contextPkg.Scope().Lookup("CancelFunc").Type()
	contextInterface, ok := types.Unalias(contextType).Underlying().(*types.Interface)
	if !ok {
		t.Fatalf("context.Context underlying type is %T, want interface", contextType.Underlying())
	}

	detector := contextFieldDetector{
		contextInterface: contextInterface,
		cancelType:       cancelType,
	}
	var violations []string
	for _, pkg := range loaded {
		if pkg.Types == nil || !isSemStreamsPackage(pkg.PkgPath) {
			continue
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				structNode, ok := node.(*ast.StructType)
				if !ok {
					return true
				}
				structType, ok := pkg.TypesInfo.TypeOf(structNode).Underlying().(*types.Struct)
				if !ok {
					return true
				}
				for i := 0; i < structType.NumFields(); i++ {
					field := structType.Field(i)
					if reason := detector.contextReason(field.Type(), make(map[types.Type]bool)); reason != "" {
						violations = append(violations, formatContextViolation(root, pkg, field, reason))
					}
					if field.Exported() && detector.isCancelFunc(field.Type()) {
						violations = append(violations, formatContextViolation(root, pkg, field, "exports context.CancelFunc"))
					}
				}
				return true
			})
		}
	}

	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("production context ownership violations:\n  %s", strings.Join(violations, "\n  "))
	}
}

type contextFieldDetector struct {
	contextInterface *types.Interface
	cancelType       types.Type
}

func TestContextFieldDetectorMatrix(t *testing.T) {
	t.Parallel()

	contextPkg, err := importer.Default().Import("context")
	if err != nil {
		t.Fatalf("import context: %v", err)
	}
	contextType := contextPkg.Scope().Lookup("Context").Type()
	cancelType := contextPkg.Scope().Lookup("CancelFunc").Type()
	detector := contextFieldDetector{
		contextInterface: contextType.Underlying().(*types.Interface),
		cancelType:       cancelType,
	}
	fixturePkg := types.NewPackage(semstreamsModulePath+"/contextfixture", "contextfixture")
	named := func(name string, underlying types.Type) *types.Named {
		return types.NewNamed(types.NewTypeName(token.NoPos, fixturePkg, name, nil), underlying, nil)
	}
	alias := func(name string, target types.Type) *types.Alias {
		return types.NewAlias(types.NewTypeName(token.NoPos, fixturePkg, name, nil), target)
	}
	provider := func(result types.Type) *types.Signature {
		results := types.NewTuple(types.NewVar(token.NoPos, fixturePkg, "result", result))
		return types.NewSignatureType(nil, nil, nil, nil, results, false)
	}
	callback := func(input types.Type) *types.Signature {
		params := types.NewTuple(types.NewVar(token.NoPos, fixturePkg, "input", input))
		return types.NewSignatureType(nil, nil, nil, params, nil, false)
	}
	providerInterface := func(name string, result types.Type) *types.Named {
		method := types.NewFunc(token.NoPos, fixturePkg, "Authority", provider(result))
		return named(name, types.NewInterfaceType([]*types.Func{method}, nil).Complete())
	}
	callbackInterface := func(name string, input types.Type) *types.Named {
		method := types.NewFunc(token.NoPos, fixturePkg, "Handle", callback(input))
		return named(name, types.NewInterfaceType([]*types.Func{method}, nil).Complete())
	}

	contextAlias := alias("ContextAlias", contextType)
	contextWrapper := named("ContextWrapper", types.NewStruct(
		[]*types.Var{types.NewVar(token.NoPos, fixturePkg, "ctx", contextAlias)}, nil,
	))
	contextProviderResult := named("ContextProviderResult", contextType.Underlying())
	for _, test := range []struct {
		name string
		typ  types.Type
		want bool
	}{
		{name: "direct", typ: contextType, want: true},
		{name: "alias", typ: contextAlias, want: true},
		{name: "pointer", typ: types.NewPointer(contextType), want: true},
		{name: "array", typ: types.NewArray(contextAlias, 1), want: true},
		{name: "slice", typ: types.NewSlice(contextWrapper), want: true},
		{name: "map", typ: types.NewMap(types.Typ[types.String], contextType), want: true},
		{name: "channel", typ: types.NewChan(types.SendRecv, contextAlias), want: true},
		{name: "struct wrapper", typ: contextWrapper, want: true},
		{name: "provider", typ: provider(contextAlias), want: true},
		{name: "interface provider named result", typ: providerInterface("ContextProvider", contextProviderResult), want: true},
		{name: "input callback", typ: callback(contextType), want: false},
		{name: "input interface", typ: callbackInterface("ContextCallback", contextType), want: false},
	} {
		t.Run("context/"+test.name, func(t *testing.T) {
			got := detector.contextReason(test.typ, make(map[types.Type]bool)) != ""
			if got != test.want {
				t.Fatalf("context authority detected = %t, want %t", got, test.want)
			}
		})
	}

	cancelAlias := alias("CancelAlias", cancelType)
	cancelWrapper := named("CancelWrapper", types.NewStruct(
		[]*types.Var{types.NewVar(token.NoPos, fixturePkg, "Cancel", cancelAlias)}, nil,
	))
	for _, test := range []struct {
		name string
		typ  types.Type
		want bool
	}{
		{name: "direct", typ: cancelType, want: true},
		{name: "alias", typ: cancelAlias, want: true},
		{name: "pointer", typ: types.NewPointer(cancelType), want: true},
		{name: "array", typ: types.NewArray(cancelAlias, 1), want: true},
		{name: "slice", typ: types.NewSlice(cancelWrapper), want: true},
		{name: "map", typ: types.NewMap(types.Typ[types.String], cancelType), want: true},
		{name: "channel", typ: types.NewChan(types.SendRecv, cancelAlias), want: true},
		{name: "struct wrapper", typ: cancelWrapper, want: true},
		{name: "provider", typ: provider(cancelAlias), want: true},
		{name: "interface provider", typ: providerInterface("CancelProvider", cancelAlias), want: true},
		{name: "input callback", typ: callback(cancelType), want: false},
		{name: "input interface", typ: callbackInterface("CancelCallback", cancelType), want: false},
		{name: "ordinary no-argument callback", typ: types.NewSignatureType(nil, nil, nil, nil, nil, false), want: false},
	} {
		t.Run("cancel/"+test.name, func(t *testing.T) {
			got := detector.isCancelFunc(test.typ)
			if got != test.want {
				t.Fatalf("cancel authority detected = %t, want %t", got, test.want)
			}
		})
	}
}

func (d contextFieldDetector) contextReason(typ types.Type, seen map[types.Type]bool) string {
	typ = types.Unalias(typ)
	if seen[typ] {
		return ""
	}
	seen[typ] = true

	if types.Implements(typ, d.contextInterface) {
		return "stores context.Context"
	}

	switch candidate := typ.(type) {
	case *types.Pointer:
		return d.contextReason(candidate.Elem(), seen)
	case *types.Named:
		underlying := candidate.Underlying()
		_, externalStruct := underlying.(*types.Struct)
		if !externalStruct || (candidate.Obj().Pkg() != nil && isSemStreamsPackage(candidate.Obj().Pkg().Path())) {
			return d.contextReason(underlying, seen)
		}
	case *types.Array:
		return d.contextReason(candidate.Elem(), seen)
	case *types.Slice:
		return d.contextReason(candidate.Elem(), seen)
	case *types.Map:
		if reason := d.contextReason(candidate.Key(), seen); reason != "" {
			return reason
		}
		return d.contextReason(candidate.Elem(), seen)
	case *types.Chan:
		return d.contextReason(candidate.Elem(), seen)
	case *types.Struct:
		for i := 0; i < candidate.NumFields(); i++ {
			if reason := d.contextReason(candidate.Field(i).Type(), seen); reason != "" {
				return reason
			}
		}
	case *types.Signature:
		for i := 0; i < candidate.Results().Len(); i++ {
			if reason := d.contextReason(candidate.Results().At(i).Type(), seen); reason != "" {
				return "provides context.Context"
			}
		}
	case *types.Interface:
		candidate.Complete()
		for i := 0; i < candidate.NumMethods(); i++ {
			if reason := d.contextReason(candidate.Method(i).Type(), seen); reason != "" {
				return "provides context.Context"
			}
		}
	}
	return ""
}

func (d contextFieldDetector) isCancelFunc(typ types.Type) bool {
	return d.containsCancelFunc(typ, make(map[types.Type]bool))
}

func (d contextFieldDetector) containsCancelFunc(typ types.Type, seen map[types.Type]bool) bool {
	typ = types.Unalias(typ)
	if seen[typ] {
		return false
	}
	seen[typ] = true

	if types.Identical(typ, types.Unalias(d.cancelType)) {
		return true
	}

	switch candidate := typ.(type) {
	case *types.Pointer:
		return d.containsCancelFunc(candidate.Elem(), seen)
	case *types.Named:
		underlying := candidate.Underlying()
		_, externalStruct := underlying.(*types.Struct)
		if !externalStruct || (candidate.Obj().Pkg() != nil && isSemStreamsPackage(candidate.Obj().Pkg().Path())) {
			return d.containsCancelFunc(underlying, seen)
		}
	case *types.Array:
		return d.containsCancelFunc(candidate.Elem(), seen)
	case *types.Slice:
		return d.containsCancelFunc(candidate.Elem(), seen)
	case *types.Map:
		return d.containsCancelFunc(candidate.Key(), seen) || d.containsCancelFunc(candidate.Elem(), seen)
	case *types.Chan:
		return d.containsCancelFunc(candidate.Elem(), seen)
	case *types.Struct:
		for i := 0; i < candidate.NumFields(); i++ {
			field := candidate.Field(i)
			if (field.Exported() || field.Embedded()) && d.containsCancelFunc(field.Type(), seen) {
				return true
			}
		}
	case *types.Signature:
		for i := 0; i < candidate.Results().Len(); i++ {
			if d.containsCancelFunc(candidate.Results().At(i).Type(), seen) {
				return true
			}
		}
	case *types.Interface:
		candidate.Complete()
		for i := 0; i < candidate.NumMethods(); i++ {
			method := candidate.Method(i)
			if method.Exported() && d.containsCancelFunc(method.Type(), seen) {
				return true
			}
		}
	}
	return false
}

func packageErrors(pkgs []*packages.Package) []string {
	var errs []string
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			errs = append(errs, err.Error())
		}
	})
	sort.Strings(errs)
	return errs
}

func importedPackage(pkgs []*packages.Package, path string) *types.Package {
	var found *types.Package
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		if pkg.Types != nil && pkg.Types.Path() == path {
			found = pkg.Types
			return false
		}
		return found == nil
	}, nil)
	return found
}

func isSemStreamsPackage(path string) bool {
	return path == semstreamsModulePath || strings.HasPrefix(path, semstreamsModulePath+"/")
}

func formatContextViolation(root string, pkg *packages.Package, field *types.Var, reason string) string {
	position := pkg.Fset.Position(field.Pos())
	path, err := filepath.Rel(root, position.Filename)
	if err != nil {
		path = position.Filename
	}
	return fmt.Sprintf("%s:%d: field %s %s", filepath.ToSlash(path), position.Line, field.Name(), reason)
}
