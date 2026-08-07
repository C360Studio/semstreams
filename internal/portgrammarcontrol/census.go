package portgrammarcontrol

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

var portLanes = map[string]struct{}{
	"inputs": {}, "outputs": {}, "kv_read": {}, "kv_write": {},
}

// Census measures the current configuration rows and executable Go constructions.
func Census(root string) (*Population, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	configItems, err := censusConfigs(absRoot)
	if err != nil {
		return nil, err
	}
	goItems, err := censusGo(absRoot)
	if err != nil {
		return nil, err
	}
	items := append(configItems, goItems...)
	sort.Slice(items, func(i, j int) bool { return items[i].RecordID < items[j].RecordID })
	return &Population{Items: items}, nil
}

func censusConfigs(root string) ([]WorkItem, error) {
	var paths []string
	err := filepath.WalkDir(filepath.Join(root, "configs"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	items := make([]WorkItem, 0, 522)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		document, err := decodeJSON(data)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		walkConfigRows(filepath.ToSlash(rel), document, nil, &items)
	}
	return items, nil
}

func walkConfigRows(path string, node any, pointer []string, items *[]WorkItem) {
	switch value := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := value[key]
			childPointer := appendPointer(pointer, key)
			if _, isLane := portLanes[key]; isLane {
				if rows, ok := child.([]any); ok {
					for index, raw := range rows {
						row, ok := raw.(map[string]any)
						if !ok {
							continue
						}
						kind := stringValue(row["type"])
						if kind == "" {
							continue
						}
						rowPointer := appendPointer(childPointer, strconv.Itoa(index))
						currentData, _ := compactJSON(row)
						classification := "mechanical"
						if kind == "kv" || kind == "kv-read" || kind == "http" {
							classification = "adjudicated"
						}
						pointerString := jsonPointer(rowPointer)
						*items = append(*items, WorkItem{
							RecordID:       "config:" + path + "#" + pointerString,
							RecordType:     "config",
							Path:           path,
							Pointer:        pointerString,
							Enclosing:      enclosingComponent(rowPointer),
							Lane:           key,
							Ordinal:        index,
							Name:           stringValue(row["name"]),
							CurrentKind:    kind,
							CurrentData:    currentData,
							Classification: classification,
							SourceSHA256:   sha256Hex([]byte(currentData)),
						})
					}
					continue
				}
			}
			walkConfigRows(path, child, childPointer, items)
		}
	case []any:
		for index, child := range value {
			walkConfigRows(path, child, appendPointer(pointer, strconv.Itoa(index)), items)
		}
	}
}

func enclosingComponent(pointer []string) string {
	for index := 0; index+1 < len(pointer); index++ {
		if pointer[index] == "components" {
			return pointer[index+1]
		}
	}
	return "<document>"
}

func appendPointer(pointer []string, segment string) []string {
	result := make([]string, len(pointer), len(pointer)+1)
	copy(result, pointer)
	return append(result, segment)
}

func jsonPointer(segments []string) string {
	encoded := make([]string, len(segments))
	for index, segment := range segments {
		segment = strings.ReplaceAll(segment, "~", "~0")
		encoded[index] = strings.ReplaceAll(segment, "/", "~1")
	}
	return "/" + strings.Join(encoded, "/")
}

func censusGo(root string) ([]WorkItem, error) {
	cache := filepath.Join(os.TempDir(), "semstreams-foundation-b-gocache")
	environment := append(os.Environ(), "GOCACHE="+cache)
	config := &packages.Config{
		Dir: root,
		Env: environment,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Tests: false,
	}
	loaded, err := packages.Load(config, "./...")
	if err != nil {
		return nil, fmt.Errorf("load Go packages: %w", err)
	}
	if count := packages.PrintErrors(loaded); count != 0 {
		return nil, fmt.Errorf("load Go packages: %d package errors", count)
	}
	items := make([]WorkItem, 0, 124)
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			position := pkg.Fset.Position(file.Pos())
			rel, err := filepath.Rel(root, position.Filename)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			data, err := os.ReadFile(position.Filename)
			if err != nil {
				return nil, err
			}
			ordinal := 0
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				enclosing := functionIdentity(pkg.Fset, function)
				ast.Inspect(function.Body, func(node ast.Node) bool {
					literal, ok := node.(*ast.CompositeLit)
					if !ok || !isPortDefinition(pkg.TypesInfo, literal) {
						return true
					}
					literalPosition := pkg.Fset.Position(literal.Pos())
					if rel == "component/schema_tags.go" && literalPosition.Line == 705 {
						return true
					}
					endPosition := pkg.Fset.Position(literal.End())
					if literalPosition.Offset < 0 || endPosition.Offset > len(data) || literalPosition.Offset >= endPosition.Offset {
						return true
					}
					fragment := data[literalPosition.Offset:endPosition.Offset]
					kind := keyedString(pkg.TypesInfo, literal, "Type")
					name := keyedString(pkg.TypesInfo, literal, "Name")
					quotedFragment, _ := compactJSON(string(fragment))
					pointer := fmt.Sprintf("L%dC%d", literalPosition.Line, literalPosition.Column)
					items = append(items, WorkItem{
						RecordID:       "go:" + rel + "#" + pointer,
						RecordType:     "go",
						Path:           rel,
						Pointer:        pointer,
						Enclosing:      enclosing,
						Lane:           "construction",
						Ordinal:        ordinal,
						Name:           name,
						CurrentKind:    kind,
						CurrentData:    quotedFragment,
						Classification: "go-construction",
						SourceLine:     literalPosition.Line,
						SourceColumn:   literalPosition.Column,
						SourceSHA256:   sha256Hex(fragment),
					})
					ordinal++
					return true
				})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].RecordID < items[j].RecordID })
	return items, nil
}

func isPortDefinition(info *types.Info, literal *ast.CompositeLit) bool {
	typeOf := info.TypeOf(literal)
	if pointer, ok := typeOf.(*types.Pointer); ok {
		typeOf = pointer.Elem()
	}
	named, ok := typeOf.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "github.com/c360studio/semstreams/component" && named.Obj().Name() == "PortDefinition"
}

func keyedString(info *types.Info, literal *ast.CompositeLit, field string) string {
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		identifier, ok := keyValue.Key.(*ast.Ident)
		if !ok || identifier.Name != field {
			continue
		}
		if value := info.Types[keyValue.Value].Value; value != nil && value.Kind() == constant.String {
			return constant.StringVal(value)
		}
	}
	return "<dynamic>"
}

func functionIdentity(fileSet *token.FileSet, function *ast.FuncDecl) string {
	position := fileSet.Position(function.Pos())
	identity := function.Name.Name + "@L" + strconv.Itoa(position.Line)
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return identity
	}
	var receiver strings.Builder
	if err := format.Node(&receiver, fileSet, function.Recv.List[0].Type); err != nil {
		return "method." + identity
	}
	return "method." + receiver.String() + "." + identity
}
