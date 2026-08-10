package apiref

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/packages"
)

func isContextType(value types.Type) bool {
	named, ok := value.(*types.Named)

	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

func strconvItoa(value int) string {
	if value == 0 {
		return "0"
	}

	var buffer [20]byte
	index := len(buffer)

	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}

	return string(buffer[index:])
}

func unwindAddChain(call *ast.CallExpr) (*ast.CallExpr, []addEntry, bool) {
	var reversed []addEntry
	current := call

	for {
		selector, ok := current.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Add" || len(current.Args) != 2 {
			return nil, nil, false
		}

		reversed = append(reversed, addEntry{name: current.Args[0], function: current.Args[1]})
		previous, ok := selector.X.(*ast.CallExpr)
		if !ok {
			return nil, nil, false
		}

		previousSelector, ok := previous.Fun.(*ast.SelectorExpr)
		if !ok || previousSelector.Sel.Name != "Add" {
			for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
				reversed[left], reversed[right] = reversed[right], reversed[left]
			}

			return previous, reversed, true
		}

		current = previous
	}
}

func isRuntimeFunctionBuilderMethod(pkg *packages.Package, expression ast.Expr, names ...string) bool {
	function := referencedFunction(pkg, expression)
	if function == nil || function.Pkg() == nil || function.Pkg().Path() != runtimePackagePath {
		return false
	}

	for _, name := range names {
		if function.Name() == name {
			return true
		}
	}

	return false
}

func referencedFunction(pkg *packages.Package, expression ast.Expr) *types.Func {
	expression = unwrapGeneric(unwrapParens(expression))

	switch value := expression.(type) {
	case *ast.Ident:
		function, _ := pkg.TypesInfo.ObjectOf(value).(*types.Func)

		return function
	case *ast.SelectorExpr:
		if selection := pkg.TypesInfo.Selections[value]; selection != nil {
			function, _ := selection.Obj().(*types.Func)
			return function
		}

		function, _ := pkg.TypesInfo.ObjectOf(value.Sel).(*types.Func)

		return function
	default:
		return nil
	}
}

func isPackageFunction(pkg *packages.Package, expression ast.Expr, packagePath, name string) bool {
	function := referencedFunction(pkg, expression)

	return function != nil && function.Pkg() != nil && function.Pkg().Path() == packagePath && function.Name() == name
}

func constantString(pkg *packages.Package, expression ast.Expr) (string, bool) {
	value := pkg.TypesInfo.Types[expression].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}

	return constant.StringVal(value), true
}

func namedPackageType(value types.Type, packagePath, name string) bool {
	if pointer, ok := value.(*types.Pointer); ok {
		value = pointer.Elem()
	}

	named, ok := value.(*types.Named)

	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == packagePath && named.Obj().Name() == name
}

func unwrapParens(expression ast.Expr) ast.Expr {
	for {
		parenthesized, ok := expression.(*ast.ParenExpr)
		if !ok {
			return expression
		}

		expression = parenthesized.X
	}
}

func unwrapGeneric(expression ast.Expr) ast.Expr {
	switch value := expression.(type) {
	case *ast.IndexExpr:
		return value.X
	case *ast.IndexListExpr:
		return value.X
	default:
		return expression
	}
}

func cloneEnvironment(source environment) environment {
	cloned := make(environment, len(source))

	for object, value := range source {
		if value.namespaces != nil {
			value.namespaces = append([]string(nil), value.namespaces...)
		}

		if value.definitions != nil {
			value.definitions = append([]functionDefinition(nil), value.definitions...)
		}

		cloned[object] = value
	}

	return cloned
}

func describeType(value types.Type) string {
	if value == nil {
		return "<unknown>"
	}

	return types.TypeString(value, func(pkg *types.Package) string { return pkg.Path() })
}

func singleValueReturns(body *ast.BlockStmt) ([]ast.Expr, bool) {
	return selectedReturns(body, 0, 1, false)
}

func selectedReturns(body *ast.BlockStmt, resultIndex, resultCount int, skipNil bool) ([]ast.Expr, bool) {
	var returns []ast.Expr
	valid := true

	ast.Inspect(body, func(current ast.Node) bool {
		if _, nested := current.(*ast.FuncLit); nested {
			return false
		}

		statement, ok := current.(*ast.ReturnStmt)
		if !ok {
			return true
		}

		if len(statement.Results) != resultCount {
			valid = false
			return false
		}

		expression := unwrapParens(statement.Results[resultIndex])
		if identifier, ok := expression.(*ast.Ident); !skipNil || !ok || identifier.Name != "nil" {
			returns = append(returns, expression)
		}

		return false
	})

	return returns, valid
}

func bindFieldList(pkg *packages.Package, fields *ast.FieldList, arguments []abstractValue, environment environment) {
	if fields == nil {
		return
	}

	argument := 0

	for _, field := range fields.List {
		for _, name := range field.Names {
			if object := pkg.TypesInfo.Defs[name]; object != nil && argument < len(arguments) {
				if _, variadic := field.Type.(*ast.Ellipsis); variadic {
					environment[object] = mergeAbstractValues(arguments[argument:]...)
					argument = len(arguments)

					continue
				}

				environment[object] = arguments[argument]
			}

			argument++
		}
	}
}

func isSDKBindFunction(function *types.Func) bool {
	if function == nil || function.Pkg() == nil || function.Pkg().Path() != sdkPackagePath {
		return false
	}

	switch function.Name() {
	case "Bind", "Bind0", "Bind1", "Bind2", "Bind3", "Bind4":
		return true
	default:
		return false
	}
}
