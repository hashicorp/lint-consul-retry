package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

var (
	broken   = make([]string, 0)
	exitCode = 0
	fset     = token.NewFileSet()
	failers  = map[string]bool{
		"Error":   true,
		"Errorf":  true,
		"Fail":    true,
		"FailNow": true,
		"Fatal":   true,
		"Fatalf":  true,
	}
	retryPath    = "\"github.com/hashicorp/consul/sdk/testutil/retry\""
	retryDepth   = 0
	requireDepth = 0
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		report(err)
	}
	if err := walkDir(dir); err != nil {
		report(err)
	}
	os.Exit(exitCode)
}

type visitor struct {
	depth       int
	currentTest string
}

func (v visitor) Visit(n ast.Node) ast.Visitor {
	// fmt.Printf("%s%T\n", strings.Repeat(" ", v.depth), n)

	// Walk uses DFS so reset when we pop back up
	if retryDepth > 0 && v.depth <= retryDepth {
		retryDepth = 0
	}
	if requireDepth > 0 && v.depth <= requireDepth {
		requireDepth = 0
	}

	switch node := n.(type) {
	case *ast.CallExpr:
		if inRequire(node) {
			requireDepth = v.depth
		}
		if inRetry(node.Fun) {
			retryDepth = v.depth
		}
		if retryDepth > 0 && callsT(node.Fun) {
			fmt.Printf("callsT: adding '%s' to broken\n", v.currentTest)
			broken = append(broken, v.currentTest)
			break
		}
		// Flag if we're using require in a retry if:
		// - t is an argument to require func
		// - require.New(t) was called earlier
		if retryDepth > 0 && usesRequire(node.Fun) {
			if usesT(node) || requireDepth > 0 {
				broken = append(broken, v.currentTest)
			}
		}
	case *ast.FuncDecl:
		name := node.Name.Name

		// Don't filter to test functions, since issue can be in helper func
		fmt.Printf("Processing: %s\n", name)
		v.currentTest = name
	}
	v.depth++
	return v
}

// impportsRetry if the source file imports retry pkg
func importsRetry(file *ast.File) bool {
	var specs []ast.Spec

	for _, decl := range file.Decls {
		if general, ok := decl.(*ast.GenDecl); ok {
			specs = general.Specs
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

// inRetry if an expression is a call to retry.Run()
func inRetry(fun ast.Expr) bool {
	function, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := function.X.(*ast.Ident)
	if !ok {
		return false
	}
	if pkg.Name == "retry" && function.Sel.Name == "Run" {
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
	firstArg, ok := ce.Args[0].(*ast.Ident)
	if !ok {
		return false
	}
	if pkg.Name == "require" && function.Sel.Name == "New" && firstArg.Name == "t" {
		return true
	}
	return false
}

// callsT checks if expression is a call to t.[Fail|Fatal|Error]
func callsT(fun ast.Expr) bool {
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

// usesT checks if t is first param of call expression
func usesT(ce *ast.CallExpr) bool {
	// t is always first arg to require when not using require.New
	firstArg, ok := ce.Args[0].(*ast.Ident)
	if !ok {
		return false
	}
	if firstArg.Name == "t" {
		return true
	}
	return false
}

func walkDir(path string) error {
	return filepath.Walk(path, visitFile)
}

func visitFile(path string, f os.FileInfo, err error) error {
	if err != nil {
		return err
	}
	if isTestFile(f) {
		f, err := parser.ParseFile(fset, f.Name(), nil, parser.ParseComments)
		if err != nil {
			fmt.Printf("failed to parse '%s' (skipping): %v", f.Name, err)
		}
		// Only process files importing sdk/testutil/retry
		if importsRetry(f) {
			v := visitor{}
			ast.Walk(v, f)
		}
	}
	return nil
}

func isTestFile(f os.FileInfo) bool {
	name := f.Name()
	return !f.IsDir() && !strings.HasPrefix(name, ".") && strings.HasSuffix(name, "_test.go")
}

func report(err error) {
	scanner.PrintError(os.Stderr, err)
	exitCode = 2
}
