package analyzer

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "goworkflows",
	Doc:      "Checks for common errors when writing workflows",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
	inspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	inspector.Nodes(nil, func(node ast.Node, push bool) bool {
		if !push {
			return false
		}

		switch n := node.(type) {
		case *ast.FuncDecl:
			// Only check functions that look like workflows
			if !isWorkflow(n) {
				return false
			}

			// Check return types
			if n.Type.Results == nil || len(n.Type.Results.List) == 0 {
				pass.Reportf(n.Pos(), "workflow `%v` doesn't return anything. needs to return at least `error`", n.Name.Name)
			} else {
				if len(n.Type.Results.List) > 2 {
					pass.Reportf(n.Pos(), "workflow `%v` returns more than two values", n.Name.Name)
					return true
				}

				lastResult := n.Type.Results.List[len(n.Type.Results.List)-1]
				if types.ExprString(lastResult.Type) != "error" {
					pass.Reportf(n.Pos(), "workflow `%v` doesn't return `error` as last return value", n.Name.Name)
				}
			}

			funcScope := pass.TypesInfo.Scopes[n.Type]
			if funcScope != nil {
				checkVarsInScope(pass, funcScope)
			}

			// Continue with the function's children
			return true

		case *ast.RangeStmt:
			{
				t := pass.TypesInfo.TypeOf(n.X)
				if t == nil {
					break
				}

				switch t.(type) {
				case *types.Map:
					pass.Reportf(n.Pos(), "iterating over a `map` is not deterministic and not allowed in workflows")

				case *types.Chan:
					pass.Reportf(n.Pos(), "using native channels is not allowed in workflows, use `workflow.Channel` instead")
				}

				// checkStatements(pass, n.Body.List)
			}

		case *ast.SelectStmt:
			pass.Reportf(n.Pos(), "`select` statements are not allowed in workflows, use `workflow.Select` instead")

		case *ast.GoStmt:
			pass.Reportf(n.Pos(), "use `workflow.Go` instead of `go` in workflows")

		case *ast.CallExpr:
			var pkg *ast.Ident
			var id *ast.Ident
			switch fun := n.Fun.(type) {
			case *ast.SelectorExpr:
				pkg, _ = fun.X.(*ast.Ident)
				id = fun.Sel
			}

			if pkg == nil || id == nil {
				break
			}

			pkgInfo := pass.TypesInfo.Uses[pkg]
			pkgName, _ := pkgInfo.(*types.PkgName)
			if pkgName == nil {
				break
			}

			switch pkgName.Imported().Path() {
			case "time":
				switch id.Name {
				case "Now":
					pass.Reportf(n.Pos(), "`time.Now` is not allowed in workflows, use `workflow.Now` instead")
				case "Sleep":
					pass.Reportf(n.Pos(), "`time.Sleep` is not allowed in workflows, use `workflow.Sleep` instead")
				}
			}
		}

		// Continue with the children
		return true
	})

	return nil, nil
}

func checkVarsInScope(pass *analysis.Pass, scope *types.Scope) {
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		switch t := obj.Type().(type) {
		case *types.Chan:
			pass.Reportf(obj.Pos(), "using native channels is not allowed in workflows, use `workflow.Channel` instead")

		case *types.Named:
			checkNamed(pass, obj, t)

		case *types.Pointer:
			if named, ok := t.Elem().(*types.Named); ok {
				checkNamed(pass, obj, named)
			}
		}
	}

	for i := 0; i < scope.NumChildren(); i++ {
		checkVarsInScope(pass, scope.Child(i))
	}
}

func checkNamed(pass *analysis.Pass, ref types.Object, named *types.Named) {
	if obj := named.Obj(); obj != nil {
		if pkg := obj.Pkg(); pkg != nil {
			fmt.Println(pkg.Path(), obj.Name(), obj.Id())

			switch pkg.Path() {
			case "sync":
				if obj.Name() == "WaitGroup" {
					pass.Reportf(ref.Pos(), "using `sync.WaitGroup` is not allowed in workflows, use `workflow.WaitGroup` instead")
				}
			}
		}
	}

}

func isWorkflow(funcDecl *ast.FuncDecl) bool {
	params := funcDecl.Type.Params.List

	// Need at least workflow.Context
	if len(params) < 1 {
		return false
	}

	firstParam, ok := params[0].Type.(*ast.SelectorExpr)
	if !ok { // first param type isn't identificator so it can't be of type "string"
		return false
	}

	xname, ok := firstParam.X.(*ast.Ident)
	if !ok {
		return false
	}

	selname := firstParam.Sel.Name
	if xname.Name+"."+selname != "workflow.Context" {
		return false
	}

	return true
}
