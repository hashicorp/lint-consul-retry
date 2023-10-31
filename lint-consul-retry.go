// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	broken  = make(map[string]map[string][]token.Pos) // Stored in a map for deduplication file->test-><nop>
	fset    = token.NewFileSet()
	failers = map[string]bool{
		"Error":   true,
		"Errorf":  true,
		"Fail":    true,
		"FailNow": true,
		"Fatal":   true,
		"Fatalf":  true,
	}
)

const (
	retryPath   = `"github.com/hashicorp/consul/sdk/testutil/retry"`
	testingPath = `"testing"`
)

func main() {
	exitCode, err := run()
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	} else {
		os.Exit(exitCode)
	}
}

func run() (int, error) {
	dir, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("failed to get cwd: %w", err)
	}
	if err := filepath.Walk(dir, visitFile); err != nil {
		return 0, fmt.Errorf("failed to walk directory: %w", err)
	}
	if len(broken) > 0 {
		os.Stderr.WriteString("Found tests using testing.T inside retry.Run:\n")
		for _, path := range keys(broken) {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				rel = path // just skip truncation
			}
			os.Stderr.WriteString(fmt.Sprintf("  %s:\n", rel))

			testList := broken[path]
			for _, test := range keys(testList) {
				os.Stderr.WriteString(fmt.Sprintf("    %s\n", test))
				for _, pos := range testList[test] {
					p := fset.Position(pos)
					os.Stderr.WriteString(fmt.Sprintf("      %s\n", p.String()))
				}
			}
		}
		return 1, nil
	}
	return 0, nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func rememberTest(path, test string, pos token.Pos) {
	testList, ok := broken[path]
	if !ok {
		testList = make(map[string][]token.Pos)
		broken[path] = testList
	}
	testList[test] = append(testList[test], pos)
}

type visitor struct {
	depth       int
	currentTest string
	path        string
	retryDepth  int
}

func (v visitor) Visit(n ast.Node) ast.Visitor {
	if n != nil {
		// When called with a non-nil ast.Node, we are delving 1 node deeper into the tree
		// and therefore should update our tracked depth accordingly.
		v.depth++
	} else {
		// Once a sub-tree of the AST has finished being walked Visit(nil) will be invoked.
		// There is no need to decrement the depth because we are passing the visitor by
		// value rather than a reference. The previous visitor on up the stack with the correct
		// depth would be used for subsequent recursive Visit calls.
		return v
	}

	switch node := n.(type) {
	case *ast.CallExpr:
		// Track whether we are in a retry block already. This is for the special case
		// of nested retry blocks which likely not a great idea but regardless we want
		// to catch incorrect usage of *testing.T.
		retrying := v.retryDepth > 0

		// Is the current function call invoking a retry. If so record the latest retry depth.
		// Note that this explicitly does not set retrying to true because that function invocation
		// SHOULD use the *testing.T.
		if inRetry(node) {
			v.retryDepth = v.depth
		}

		// Alert if we are using a *testing.T within a retry.Run* invocation.
		//
		// This will catch uses of methods on the object such as calling t.Fail() and will
		// also catch passing the *testing.T as an argument to another function.
		if retrying && usesTestingT(node) {
			rememberTest(v.path, v.currentTest, node.Pos())
		}
	case *ast.FuncDecl:
		// Record the function name for reporting when the functions code fails linting. Note
		// that we don't want to filter to test functions only as sub-tests and helpers could
		// be where the incorrect *testing.T usage comes from
		v.currentTest = node.Name.Name
	}
	return v
}

// importsPackage will check if the given file imports the package with the specified path
func importsPackage(file *ast.File, importPkg string) bool {
	var specs []ast.Spec

	for _, decl := range file.Decls {
		if general, ok := decl.(*ast.GenDecl); ok {
			specs = append(specs, general.Specs...)
		}
	}
	for _, spec := range specs {
		pkg, ok := spec.(*ast.ImportSpec)
		if !ok {
			continue
		}
		path := pkg.Path.Value
		if path == importPkg {
			return true
		}
	}
	return false
}

// inRetry returns true if an expression is a call to retry.Run(t func(r *retry.R){...})
func inRetry(ce *ast.CallExpr) bool {
	function, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := function.X.(*ast.Ident)
	if !ok {
		return false
	}

	if pkg.Name != "retry" {
		return false
	}

	var lit *ast.FuncLit
	switch function.Sel.Name {
	case "Run": // retry.Run(t, <FUNC>)
		var ok bool
		lit, ok = ce.Args[1].(*ast.FuncLit)
		if !ok {
			return false
		}
	case "RunWith": // retry.RunWith(<FAILER>, t, <FUNC>)
		var ok bool
		lit, ok = ce.Args[2].(*ast.FuncLit)
		if !ok {
			return false
		}
	default:
		return false
	}

	// Check for 'r' because 'retry.Run(t func(t *retry.R){...})' is valid
	param := lit.Type.Params.List[0]
	if param.Names[0].Name == "r" {
		return true
	}
	return false
}

// objectIsTestingT is a helper method to identify that an object is a concreate *testing.T type.
func objectIsTestingT(obj *ast.Object) bool {
	if obj == nil {
		return false
	}

	field, ok := obj.Decl.(*ast.Field)
	if !ok {
		return false
	}

	ptr, ok := field.Type.(*ast.StarExpr)
	if !ok {
		return false
	}

	sel, ok := ptr.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "T" {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	return pkg.Name == "testing"
}

// usesTestingT will check if a *testing.T is passed as an argument to the
// call or whether the CallExpr represents the invocation of a method on
// the *testing.T type itself.
func usesTestingT(ce *ast.CallExpr) bool {
	if sel, ok := ce.Fun.(*ast.SelectorExpr); ok {
		receiver, ok := sel.X.(*ast.Ident)
		if ok && objectIsTestingT(receiver.Obj) {
			return true
		}
	}

	for _, raw := range ce.Args {
		arg, ok := raw.(*ast.Ident)
		if !ok {
			continue
		}

		if arg.Obj == nil || arg.Obj.Kind != ast.Var {
			continue
		}

		if objectIsTestingT(arg.Obj) {
			return true
		}
	}

	return false
}

func visitFile(path string, f os.FileInfo, err error) error {
	if err != nil {
		return fmt.Errorf("failed to visit '%s', %v", path, err)
	}

	// Note that we do not want to restrict to _test.go files only as there
	// can be retry issues non _test.go files in test-only packages. Instead
	// we will check if the package imports "testing".
	if !isGoFile(path, f) {
		return nil
	}

	tree, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse test file '%s', %v", path, err)
	}

	// Only process files importing sdk/testutil/retry
	if importsPackage(tree, retryPath) && importsPackage(tree, testingPath) {
		v := visitor{path: path}
		ast.Walk(v, tree)
	}

	return nil
}

func isGoFile(path string, f os.FileInfo) bool {
	if !f.Mode().IsRegular() {
		return false
	}
	return strings.HasSuffix(path, ".go")
}
