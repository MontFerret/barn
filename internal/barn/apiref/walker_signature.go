package apiref

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/packages"
)

func (w *walker) signature(pkg *packages.Package, expression ast.Expr, forced *signatureShape, active map[*types.Func]bool) (signatureRecord, error) {
	original := expression
	documentation := ""

	if call, ok := unwrapParens(expression).(*ast.CallExpr); ok {
		if function := referencedFunction(pkg, call.Fun); isSDKBindFunction(function) {
			if len(call.Args) != 1 {
				return signatureRecord{}, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "sdk.Bind registration does not contain one statically resolvable function")
			}

			expression = call.Args[0]
		}
	}

	signature, declaration, _, err := w.resolveSignature(pkg, expression, active)
	if err != nil {
		return signatureRecord{}, err
	}

	if declaration != nil && declaration.decl.Doc != nil {
		documentation = strings.TrimSpace(declaration.decl.Doc.Text())
	}

	parameters := signature.Params()
	start := 0

	if parameters.Len() > 0 && isContextType(parameters.At(0).Type()) {
		start = 1
	}

	count := parameters.Len() - start
	variadic := signature.Variadic()
	if forced != nil {
		if forced.variadic != variadic || (!variadic && forced.arity != count) {
			return signatureRecord{}, w.source.errorAt(ErrorUnsupportedRegistration, original.Pos(), "registered function type does not match the selected function-builder arity")
		}
	} else if count > 4 && !variadic {
		return signatureRecord{}, w.source.errorAt(ErrorUnsupportedRegistration, original.Pos(), "registered fixed-arity function has more than four Ferret parameters")
	}

	if variadic && count != 1 {
		return signatureRecord{}, w.source.errorAt(ErrorUnsupportedRegistration, original.Pos(), "registered variadic function must expose exactly one Ferret variadic parameter")
	}

	names := make([]string, 0, count)
	for index := start; index < parameters.Len(); index++ {
		name := parameters.At(index).Name()

		if name == "" || name == "_" {
			name = "arg" + strconvItoa(index-start+1)
		}

		names = append(names, name)
	}

	return signatureRecord{parameters: names, variadic: variadic, documentation: documentation}, nil
}

func (w *walker) resolveSignature(pkg *packages.Package, expression ast.Expr, active map[*types.Func]bool) (*types.Signature, *functionNode, any, error) {
	expression = unwrapParens(expression)

	if function := referencedFunction(pkg, expression); function != nil {
		signature, _ := function.Type().(*types.Signature)
		if signature == nil {
			return nil, nil, nil, w.source.errorAt(ErrorUnresolvedTarget, expression.Pos(), "registered declaration is not a function")
		}

		if node, exists := w.source.functions[function]; exists {
			return signature, &node, function, nil
		}

		return signature, nil, function, nil
	}

	if literal, ok := expression.(*ast.FuncLit); ok {
		signature, _ := pkg.TypesInfo.TypeOf(literal).(*types.Signature)
		if signature == nil {
			return nil, nil, nil, w.source.errorAt(ErrorUnresolvedTarget, literal.Pos(), "registered function literal has no type information")
		}

		return signature, nil, literal, nil
	}

	if identifier, ok := expression.(*ast.Ident); ok {
		if declaration, exists := w.source.values[pkg.TypesInfo.ObjectOf(identifier)]; exists {
			return w.resolveSignature(declaration.pkg, declaration.expr, active)
		}
	}

	if call, ok := expression.(*ast.CallExpr); ok {
		function := referencedFunction(pkg, call.Fun)

		if isSDKBindFunction(function) {
			if len(call.Args) != 1 {
				return nil, nil, nil, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "sdk.Bind registration does not contain one statically resolvable function")
			}
			return w.resolveSignature(pkg, call.Args[0], active)
		}

		if function == nil || function.Pkg() == nil || !w.localPackage(function.Pkg().Path()) {
			return nil, nil, nil, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "function factory target is not a statically resolvable module-local function")
		}

		if active[function] {
			return nil, nil, nil, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "recursive function factory is unsupported")
		}

		node, exists := w.source.functions[function]
		if !exists {
			return nil, nil, nil, w.source.errorAt(ErrorUnresolvedTarget, call.Pos(), "function factory source is unavailable")
		}

		returns, valid := singleValueReturns(node.decl.Body)
		if !valid || len(returns) == 0 {
			return nil, nil, nil, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "function factory does not have statically resolvable single-value returns")
		}

		active[function] = true
		defer delete(active, function)
		var resolvedSignature *types.Signature
		var resolvedDeclaration *functionNode
		var resolvedIdentity any

		for _, result := range returns {
			signature, declaration, identity, err := w.resolveSignature(node.pkg, result, active)
			if err != nil {
				return nil, nil, nil, err
			}

			if resolvedIdentity == nil {
				resolvedSignature = signature
				resolvedDeclaration = declaration
				resolvedIdentity = identity

				continue
			}

			if resolvedIdentity != identity {
				return nil, nil, nil, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "function factory selects between multiple function targets")
			}
		}

		return resolvedSignature, resolvedDeclaration, resolvedIdentity, nil
	}

	return nil, nil, nil, w.source.errorAt(ErrorUnresolvedTarget, expression.Pos(), "registered value is not a statically resolvable named function, function literal, or factory: "+describeType(pkg.TypesInfo.TypeOf(expression)))
}
