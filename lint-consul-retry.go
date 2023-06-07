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
	broken  = make(map[string]map[string]struct{}) // Stored in a map for deduplication file->test-><nop>
	fset    = token.NewFileSet()
	failers = map[string]bool{
		"Error":   true,
		"Errorf":  true,
		"Fail":    true,
		"FailNow": true,
		"Fatal":   true,
		"Fatalf":  true,
	}

	retryDepth = 0     // tracks depth of current retry.Run call
	newRequire = false // tracks whether require.New(t) was called
)

const (
	retryPath = `"github.com/hashicorp/consul/sdk/testutil/retry"`
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
	if err := walkDir(dir); err != nil {
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

func rememberTest(path, test string) {
	testList, ok := broken[path]
	if !ok {
		testList = make(map[string]struct{})
		broken[path] = testList
	}
	testList[test] = struct{}{}
}

type visitor struct {
	depth       int
	currentTest string
	path        string
}

func (v visitor) Visit(n ast.Node) ast.Visitor {
	// Walk uses DFS so reset when we pop back up
	if retryDepth > 0 && v.depth <= retryDepth {
		retryDepth = 0
	}

	switch node := n.(type) {
	case *ast.CallExpr:
		if inRequire(node) {
			newRequire = true
		}
		if inRetry(node) {
			retryDepth = v.depth
		}
		if retryDepth > 0 && tCallsFailer(node.Fun) {
			rememberTest(v.path, v.currentTest)
			break
		}
		// Flag if we're using require in a retry if:
		// - require.New(t) was called earlier and assertion does not use 'r'
		// - t is an argument to require func
		if retryDepth > 0 && usesRequire(node.Fun) {
			if (newRequire && !usesParam("r", node)) || usesParam("t", node) {
				rememberTest(v.path, v.currentTest)
			}
		}
	case *ast.FuncDecl:
		name := node.Name.Name

		// Don't filter to test functions, since issue can be in helper func
		v.currentTest = name
		newRequire = false // Will only call require.New once per function call
	}
	v.depth++
	return v
}

// impportsRetry if the source file imports retry pkg
func importsRetry(file *ast.File) bool {
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
		if path == retryPath {
			return true
		}
	}
	return false
}

// inRetry if an expression is a call to retry.Run(t func(r *retry.R){...})
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

// inRequire if expression is a call to require.New(t)
func inRequire(ce *ast.CallExpr) bool {
	function, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := function.X.(*ast.Ident)
	if !ok {
		return false
	}
	if len(ce.Args) == 0 {
		return false
	}
	firstArg, ok := ce.Args[0].(*ast.Ident)
	if !ok {
		return false
	}
	if !(pkg.Name == "require" || pkg.Name == "assert") {
		return false
	}
	if function.Sel.Name == "New" && firstArg.Name == "t" {
		return true
	}
	return false
}

// tCallsFailer checks if expression is a call to t.[Fail|Fatal|Error]
func tCallsFailer(fun ast.Expr) bool {
	function, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := function.X.(*ast.Ident)
	if !ok {
		return false
	}
	if pkg.Name == "t" && failers[function.Sel.Name] {
		return true
	}
	return false
}

// usesRequire checks if a function call uses require/assert
func usesRequire(fun ast.Expr) bool {
	function, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := function.X.(*ast.Ident)
	if !ok {
		return false
	}
	if pkg.Name == "assert" || pkg.Name == "require" {
		return true
	}
	return false
}

// usesParam checks if param is first in a call expression
func usesParam(param string, ce *ast.CallExpr) bool {
	// t is always first arg to require when not using require.New
	firstArg, ok := ce.Args[0].(*ast.Ident)
	if !ok {
		return false
	}
	if firstArg.Name == param {
		return true
	}
	return false
}

func walkDir(path string) error {
	return filepath.Walk(path, visitFile)
}

func visitFile(path string, f os.FileInfo, err error) error {
	if err != nil {
		return fmt.Errorf("failed to visit '%s', %v", path, err)
	}

	if !isGoTestFile(path, f) {
		return nil
	}

	tree, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("failed to parse test file '%s', %v", path, err)
	}

	// Only process files importing sdk/testutil/retry
	if importsRetry(tree) {
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

func isGoTestFile(path string, f os.FileInfo) bool {
	return isGoFile(path, f) && strings.HasSuffix(path, "_test.go")
}
