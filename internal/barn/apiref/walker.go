package apiref

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

type (
	abstractValue struct {
		bootstrap   bool
		namespaces  []string
		definitions []functionDefinition
		unknown     bool
	}

	functionDefinition struct {
		pkg      *packages.Package
		name     string
		function ast.Expr
		position token.Pos
	}

	environment map[types.Object]abstractValue

	walker struct {
		source *loadedSource
		result *resultBuilder
		stack  map[*types.Func]bool
	}
)

func (w *walker) walkConstructor(node functionNode) error {
	constructor, _ := node.pkg.TypesInfo.Defs[node.decl.Name].(*types.Func)
	if constructor == nil {
		return w.source.errorAt(ErrorInternal, node.decl.Pos(), "New constructor type information is unavailable")
	}

	signature, _ := constructor.Type().(*types.Signature)
	if signature == nil {
		return w.source.errorAt(ErrorDeclarationNotFound, node.decl.Pos(), "New does not return module.Module")
	}

	moduleResult := -1
	for index := 0; index < signature.Results().Len(); index++ {
		if namedPackageType(signature.Results().At(index).Type(), modulePackagePath, "Module") {
			if moduleResult >= 0 {
				return w.source.errorAt(ErrorUnsupportedRegistration, node.decl.Pos(), "New returns more than one module.Module value")
			}
			moduleResult = index
		}
	}

	if moduleResult < 0 {
		return w.source.errorAt(ErrorDeclarationNotFound, node.decl.Pos(), "New does not return module.Module")
	}

	returns, valid := selectedReturns(node.decl.Body, moduleResult, signature.Results().Len(), true)
	if !valid || len(returns) == 0 {
		return w.source.errorAt(ErrorUnsupportedRegistration, node.decl.Pos(), "New does not have statically resolvable module return paths")
	}

	var moduleCalls []*ast.CallExpr
	localTypes := make(map[*types.Named]token.Pos)

	for _, expression := range returns {
		expression = unwrapParens(expression)
		if call, ok := expression.(*ast.CallExpr); ok && isPackageFunction(node.pkg, call.Fun, sdkPackagePath, "NewModule") {
			moduleCalls = append(moduleCalls, call)
			continue
		}

		typeOf := types.Unalias(node.pkg.TypesInfo.TypeOf(expression))
		if pointer, ok := typeOf.(*types.Pointer); ok {
			typeOf = pointer.Elem()
		}

		named, ok := typeOf.(*types.Named)
		if !ok || named.Obj().Pkg() == nil || !w.localPackage(named.Obj().Pkg().Path()) {
			return w.source.errorAt(ErrorUnsupportedRegistration, expression.Pos(), "New selects a module root dynamically")
		}

		localTypes[named] = expression.Pos()
	}

	if len(moduleCalls) > 0 {
		if len(moduleCalls) != 1 || len(localTypes) != 0 || len(returns) != 1 {
			return w.source.errorAt(ErrorUnsupportedRegistration, node.decl.Pos(), "New selects from multiple module roots")
		}

		call := moduleCalls[0]
		if len(call.Args) != 2 {
			return w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "sdk.NewModule must receive one static registration callback")
		}

		return w.walkCallable(node.pkg, call.Args[1], []abstractValue{{bootstrap: true}})
	}

	if len(localTypes) != 1 {
		return w.source.errorAt(ErrorUnsupportedRegistration, node.decl.Pos(), "New dynamically selects between multiple local module implementations")
	}

	var named *types.Named
	var position token.Pos

	for candidate, candidatePosition := range localTypes {
		named, position = candidate, candidatePosition
	}

	method, err := w.returnedRegisterMethod(named, position)
	if err != nil {
		return err
	}

	methodNode, exists := w.source.functions[method]
	if !exists {
		return w.source.errorAt(ErrorUnresolvedTarget, position, "returned module Register method source is unavailable")
	}

	return w.walkFunction(method, methodNode, []abstractValue{{bootstrap: true}})
}

func (w *walker) returnedRegisterMethod(named *types.Named, position token.Pos) (*types.Func, error) {
	selection := types.NewMethodSet(types.NewPointer(named)).Lookup(nil, "Register")
	if selection == nil {
		selection = types.NewMethodSet(named).Lookup(nil, "Register")
	}

	if selection == nil {
		return nil, w.source.errorAt(ErrorDeclarationNotFound, position, "returned local module type has no Register method")
	}

	method, ok := selection.Obj().(*types.Func)
	if !ok {
		return nil, w.source.errorAt(ErrorInternal, position, "Register method object is not a function")
	}

	signature, _ := method.Type().(*types.Signature)
	errorType := types.Universe.Lookup("error").Type()

	if signature == nil || signature.Params().Len() != 1 || !namedPackageType(signature.Params().At(0).Type(), modulePackagePath, "Bootstrap") ||
		signature.Results().Len() != 1 || !types.Identical(signature.Results().At(0).Type(), errorType) {

		return nil, w.source.errorAt(ErrorUnsupportedRegistration, position, "Register method must have signature Register(module.Bootstrap) error")
	}

	return method, nil
}

func (w *walker) walkCallable(pkg *packages.Package, expression ast.Expr, arguments []abstractValue) error {
	expression = unwrapParens(expression)

	switch value := expression.(type) {
	case *ast.FuncLit:
		return w.walkFunctionLiteral(pkg, value, arguments)
	case *ast.Ident, *ast.SelectorExpr:
		function := referencedFunction(pkg, expression)
		if function == nil {
			return w.source.errorAt(ErrorUnresolvedTarget, expression.Pos(), "registration callback does not resolve to a named function")
		}

		node, exists := w.source.functions[function]
		if !exists {
			return w.source.errorAt(ErrorUnresolvedTarget, expression.Pos(), "registration callback source is outside the module")
		}

		return w.walkFunction(function, node, arguments)
	default:
		return w.source.errorAt(ErrorUnsupportedRegistration, expression.Pos(), "registration callback is selected dynamically")
	}
}

func (w *walker) walkFunction(function *types.Func, node functionNode, arguments []abstractValue) error {
	if w.stack[function] {
		return w.source.errorAt(ErrorUnsupportedRegistration, node.decl.Pos(), "recursive registration helper is unsupported")
	}

	w.stack[function] = true
	defer delete(w.stack, function)

	env := make(environment)
	bindFieldList(node.pkg, node.decl.Type.Params, arguments, env)

	return w.walkBlock(node.pkg, node.decl.Body.List, env)
}

func (w *walker) walkFunctionLiteral(pkg *packages.Package, function *ast.FuncLit, arguments []abstractValue) error {
	env := make(environment)
	bindFieldList(pkg, function.Type.Params, arguments, env)

	return w.walkBlock(pkg, function.Body.List, env)
}

func (w *walker) walkBlock(pkg *packages.Package, statements []ast.Stmt, environment environment) error {
	for _, statement := range statements {
		if err := w.walkStatement(pkg, statement, environment); err != nil {
			return err
		}
	}

	return nil
}

func (w *walker) walkStatement(pkg *packages.Package, statement ast.Stmt, env environment) error {
	switch value := statement.(type) {
	case *ast.AssignStmt:
		for _, expression := range value.Rhs {
			if err := w.processExpression(pkg, expression, env); err != nil {
				return err
			}
		}

		w.bindAssignment(pkg, value.Lhs, value.Rhs, env)
	case *ast.DeclStmt:
		general, ok := value.Decl.(*ast.GenDecl)
		if !ok {
			return nil
		}

		for _, specification := range general.Specs {
			definition, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}

			for _, expression := range definition.Values {
				if err := w.processExpression(pkg, expression, env); err != nil {
					return err
				}
			}

			w.bindNames(pkg, definition.Names, definition.Values, env)
		}
	case *ast.ExprStmt:
		return w.processExpression(pkg, value.X, env)
	case *ast.ReturnStmt:
		for _, expression := range value.Results {
			if err := w.processExpression(pkg, expression, env); err != nil {
				return err
			}
		}
	case *ast.IfStmt:
		branchEnvironment := cloneEnvironment(env)

		if value.Init != nil {
			if err := w.walkStatement(pkg, value.Init, branchEnvironment); err != nil {
				return err
			}
		}

		if err := w.processExpression(pkg, value.Cond, branchEnvironment); err != nil {
			return err
		}

		bodyEnvironment := cloneEnvironment(branchEnvironment)

		if err := w.walkBlock(pkg, value.Body.List, bodyEnvironment); err != nil {
			return err
		}

		branches := []environment{bodyEnvironment}
		if value.Else != nil {
			elseEnvironment := cloneEnvironment(branchEnvironment)

			if err := w.walkStatement(pkg, value.Else, elseEnvironment); err != nil {
				return err
			}

			branches = append(branches, elseEnvironment)
		} else {
			branches = append(branches, branchEnvironment)
		}

		mergeEnvironmentBranches(env, branches...)
	case *ast.BlockStmt:
		blockEnvironment := cloneEnvironment(env)

		if err := w.walkBlock(pkg, value.List, blockEnvironment); err != nil {
			return err
		}

		mergeEnvironmentBranches(env, blockEnvironment)
	case *ast.ForStmt, *ast.RangeStmt:
		if w.containsRegistration(pkg, statement, env) || w.containsRegistrationMutation(pkg, statement, env) {
			return w.source.errorAt(ErrorUnsupportedRegistration, statement.Pos(), "loop-driven function registration is unsupported")
		}
	case *ast.SwitchStmt:
		branchEnvironment := cloneEnvironment(env)

		if value.Init != nil {
			if err := w.walkStatement(pkg, value.Init, branchEnvironment); err != nil {
				return err
			}
		}

		if value.Tag != nil {
			if err := w.processExpression(pkg, value.Tag, branchEnvironment); err != nil {
				return err
			}
		}

		branches := make([]environment, 0, len(value.Body.List)+1)
		hasDefault := false

		for _, stmt := range value.Body.List {
			clause, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}

			caseEnvironment := cloneEnvironment(branchEnvironment)
			hasDefault = hasDefault || len(clause.List) == 0

			for _, expression := range clause.List {
				if err := w.processExpression(pkg, expression, caseEnvironment); err != nil {
					return err
				}
			}

			if err := w.walkBlock(pkg, clause.Body, caseEnvironment); err != nil {
				return err
			}

			branches = append(branches, caseEnvironment)
		}

		if !hasDefault {
			branches = append(branches, branchEnvironment)
		}

		mergeEnvironmentBranches(env, branches...)
	case *ast.TypeSwitchStmt:
		branchEnvironment := cloneEnvironment(env)

		if value.Init != nil {
			if err := w.walkStatement(pkg, value.Init, branchEnvironment); err != nil {
				return err
			}
		}

		if value.Assign != nil {
			if err := w.walkStatement(pkg, value.Assign, branchEnvironment); err != nil {
				return err
			}
		}

		branches := make([]environment, 0, len(value.Body.List)+1)
		hasDefault := false

		for _, stmt := range value.Body.List {
			if clause, ok := stmt.(*ast.CaseClause); ok {
				caseEnvironment := cloneEnvironment(branchEnvironment)
				hasDefault = hasDefault || len(clause.List) == 0

				if err := w.walkBlock(pkg, clause.Body, caseEnvironment); err != nil {
					return err
				}

				branches = append(branches, caseEnvironment)
			}
		}

		if !hasDefault {
			branches = append(branches, branchEnvironment)
		}

		mergeEnvironmentBranches(env, branches...)
	case *ast.SelectStmt:
		if w.containsRegistration(pkg, statement, env) {
			return w.source.errorAt(ErrorUnsupportedRegistration, statement.Pos(), "control-flow-selected function registration is unsupported")
		}
	case *ast.GoStmt, *ast.DeferStmt:
		if w.containsRegistration(pkg, statement, env) {
			return w.source.errorAt(ErrorUnsupportedRegistration, statement.Pos(), "deferred or concurrent function registration is unsupported")
		}
	case *ast.LabeledStmt:
		return w.walkStatement(pkg, value.Stmt, env)
	}

	return nil
}

func (w *walker) bindAssignment(pkg *packages.Package, left []ast.Expr, right []ast.Expr, environment environment) {
	if len(left) != len(right) {
		return
	}

	for index, expression := range left {
		identifier, ok := expression.(*ast.Ident)
		if !ok || identifier.Name == "_" {
			continue
		}

		object := pkg.TypesInfo.ObjectOf(identifier)
		if object == nil {
			continue
		}

		environment[object] = w.evaluateValue(pkg, right[index], environment)
	}
}

func (w *walker) bindNames(pkg *packages.Package, names []*ast.Ident, values []ast.Expr, environment environment) {
	if len(names) != len(values) {
		return
	}

	for index, name := range names {
		if object := pkg.TypesInfo.Defs[name]; object != nil {
			environment[object] = w.evaluateValue(pkg, values[index], environment)
		}
	}
}

func (w *walker) evaluateValue(pkg *packages.Package, expression ast.Expr, environment environment) abstractValue {
	value := abstractValue{}

	if w.evaluateBootstrap(pkg, expression, environment) {
		value.bootstrap = true
	}

	if namespaces, ok := w.evaluateNamespace(pkg, expression, environment); ok {
		value.namespaces = namespaces
	}

	if definitions, ok := w.evaluateDefinitions(pkg, expression, environment, make(map[*types.Func]bool)); ok {
		value.definitions = definitions
	}

	if !value.bootstrap && value.namespaces == nil && value.definitions == nil && w.registrationValueType(pkg.TypesInfo.TypeOf(expression)) {
		value.unknown = true
	}

	return value
}

func (w *walker) processExpression(pkg *packages.Package, expression ast.Expr, environment environment) error {
	expression = unwrapParens(expression)

	switch value := expression.(type) {
	case *ast.CallExpr:
		if handled, err := w.processCall(pkg, value, environment); handled || err != nil {
			return err
		}

		if err := w.processExpression(pkg, value.Fun, environment); err != nil {
			return err
		}

		for _, argument := range value.Args {
			if err := w.processExpression(pkg, argument, environment); err != nil {
				return err
			}
		}
	case *ast.BinaryExpr:
		if err := w.processExpression(pkg, value.X, environment); err != nil {
			return err
		}

		return w.processExpression(pkg, value.Y, environment)
	case *ast.UnaryExpr:
		return w.processExpression(pkg, value.X, environment)
	case *ast.SelectorExpr:
		return w.processExpression(pkg, value.X, environment)
	case *ast.IndexExpr:
		if err := w.processExpression(pkg, value.X, environment); err != nil {
			return err
		}
		return w.processExpression(pkg, value.Index, environment)
	case *ast.IndexListExpr:
		if err := w.processExpression(pkg, value.X, environment); err != nil {
			return err
		}

		for _, index := range value.Indices {
			if err := w.processExpression(pkg, index, environment); err != nil {
				return err
			}
		}
	case *ast.SliceExpr:
		for _, item := range []ast.Expr{value.X, value.Low, value.High, value.Max} {
			if item != nil {
				if err := w.processExpression(pkg, item, environment); err != nil {
					return err
				}
			}
		}
	case *ast.TypeAssertExpr:
		return w.processExpression(pkg, value.X, environment)
	case *ast.StarExpr:
		return w.processExpression(pkg, value.X, environment)
	case *ast.CompositeLit:
		for _, element := range value.Elts {
			switch item := element.(type) {
			case *ast.KeyValueExpr:
				if err := w.processExpression(pkg, item.Key, environment); err != nil {
					return err
				}
				if err := w.processExpression(pkg, item.Value, environment); err != nil {
					return err
				}
			case ast.Expr:
				if err := w.processExpression(pkg, item, environment); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (w *walker) processCall(pkg *packages.Package, call *ast.CallExpr, environment environment) (bool, error) {
	if isPackageFunction(pkg, call.Fun, sdkPackagePath, "RegisterFunctions") {
		if len(call.Args) < 2 {
			return true, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "sdk.RegisterFunctions has no static definitions")
		}

		namespaces, ok := w.evaluateNamespace(pkg, call.Args[0], environment)
		if !ok {
			return true, w.source.errorAt(ErrorUnsupportedRegistration, call.Args[0].Pos(), "function namespace is not statically resolvable")
		}

		for _, expression := range call.Args[1:] {
			definitions, ok := w.evaluateDefinitions(pkg, expression, environment, make(map[*types.Func]bool))
			if !ok {
				return true, w.source.errorAt(ErrorUnsupportedRegistration, expression.Pos(), "function definitions are not a static sdk.Func sequence")
			}

			for _, definition := range definitions {
				signature, err := w.signature(definition.pkg, definition.function, nil, make(map[*types.Func]bool))
				if err != nil {
					return true, err
				}

				for _, namespace := range namespaces {
					if err := w.result.add(namespace, definition.name, signature); err != nil {
						return true, w.source.errorAt(ErrorUnsupportedRegistration, definition.position, err.Error())
					}
				}
			}
		}

		return true, nil
	}

	if handled, err := w.processAddChain(pkg, call, environment); handled || err != nil {
		return handled, err
	}

	if function := referencedFunction(pkg, call.Fun); function != nil && function.Pkg() != nil && function.Pkg().Path() == runtimePackagePath {
		switch function.Name() {
		case "Add":
			return true, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "function-builder Add registration is not a supported direct chain")
		case "From", "Remove":
			return true, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "function-builder From and Remove registrations are unsupported")
		}
	}

	if _, ok := w.evaluateDefinitions(pkg, call, environment, make(map[*types.Func]bool)); ok {
		return true, nil
	}

	function := referencedFunction(pkg, call.Fun)
	arguments := make([]abstractValue, len(call.Args))
	meaningful := false
	meaningfulTypes := make([]string, 0, len(call.Args))

	for index, argument := range call.Args {
		arguments[index] = w.evaluateValue(pkg, argument, environment)

		if arguments[index].unknown {
			return true, w.source.errorAt(ErrorUnsupportedRegistration, argument.Pos(), "registration helper argument is not statically resolvable")
		}

		argumentIsMeaningful := arguments[index].bootstrap || arguments[index].namespaces != nil || arguments[index].definitions != nil
		meaningful = meaningful || argumentIsMeaningful

		if argumentIsMeaningful {
			kinds := make([]string, 0, 3)

			if arguments[index].bootstrap {
				kinds = append(kinds, "bootstrap")
			}

			if arguments[index].namespaces != nil {
				kinds = append(kinds, "namespace")
			}

			if arguments[index].definitions != nil {
				kinds = append(kinds, "definitions")
			}

			meaningfulTypes = append(meaningfulTypes, describeType(pkg.TypesInfo.TypeOf(argument))+" ("+strings.Join(kinds, "+")+")")
		}
	}

	if !meaningful {
		return false, nil
	}

	if function == nil {
		return true, w.source.errorAt(ErrorUnresolvedTarget, call.Pos(), "registration helper target is dynamically dispatched with registration values of type "+strings.Join(meaningfulTypes, ", "))
	}

	if function.Pkg() == nil || !w.localPackage(function.Pkg().Path()) {
		return true, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "registration helper is outside the analyzed Go module")
	}

	node, exists := w.source.functions[function]
	if !exists {
		return true, w.source.errorAt(ErrorUnresolvedTarget, call.Pos(), "registration helper source is unavailable")
	}

	return true, w.walkFunction(function, node, arguments)
}

func (w *walker) processAddChain(pkg *packages.Package, call *ast.CallExpr, environment environment) (bool, error) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Add" || !isRuntimeFunctionBuilderMethod(pkg, call.Fun, "Add") {
		return false, nil
	}

	base, entries, ok := unwindAddChain(call)
	if !ok {
		return false, nil
	}

	aritySelector, ok := base.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, nil
	}

	variadic := aritySelector.Sel.Name == "Var"
	fixedArity := -1

	if !variadic && len(aritySelector.Sel.Name) == 2 && aritySelector.Sel.Name[0] == 'A' {
		parsed, err := strconv.Atoi(aritySelector.Sel.Name[1:])
		if err == nil && parsed >= 0 && parsed <= 4 {
			fixedArity = parsed
		}
	}

	if !variadic && fixedArity < 0 {
		return false, nil
	}

	functionCall, ok := aritySelector.X.(*ast.CallExpr)
	if !ok || len(functionCall.Args) != 0 {
		return true, w.source.errorAt(ErrorUnsupportedRegistration, call.Pos(), "function builder arity is selected dynamically")
	}

	functionSelector, ok := functionCall.Fun.(*ast.SelectorExpr)
	if !ok || functionSelector.Sel.Name != "Function" {
		return false, nil
	}

	namespaces, ok := w.evaluateNamespace(pkg, functionSelector.X, environment)
	if !ok {
		return true, w.source.errorAt(ErrorUnsupportedRegistration, functionSelector.X.Pos(), "function builder namespace is not statically resolvable")
	}

	for _, entry := range entries {
		name, ok := constantString(pkg, entry.name)
		if !ok || strings.TrimSpace(name) == "" {
			return true, w.source.errorAt(ErrorUnsupportedRegistration, entry.name.Pos(), "function builder name is not a non-empty constant string")
		}

		forced := &signatureShape{arity: fixedArity, variadic: variadic}
		signature, err := w.signature(pkg, entry.function, forced, make(map[*types.Func]bool))
		if err != nil {
			return true, err
		}

		for _, namespace := range namespaces {
			if err := w.result.add(namespace, name, signature); err != nil {
				return true, w.source.errorAt(ErrorUnsupportedRegistration, entry.name.Pos(), err.Error())
			}
		}
	}

	return true, nil
}

func (w *walker) evaluateNamespace(pkg *packages.Package, expression ast.Expr, environment environment) ([]string, bool) {
	expression = unwrapParens(expression)

	if identifier, ok := expression.(*ast.Ident); ok {
		object := pkg.TypesInfo.ObjectOf(identifier)

		if value, exists := environment[object]; exists {
			if value.namespaces != nil {
				return append([]string{}, value.namespaces...), true
			}

			if value.bootstrap {
				return nil, false
			}
		}

		if declaration, exists := w.source.values[object]; exists {
			return w.evaluateNamespace(declaration.pkg, declaration.expr, environment)
		}
	}

	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return nil, false
	}

	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}

	switch selector.Sel.Name {
	case "Host":
		if len(call.Args) == 0 && w.evaluateBootstrap(pkg, selector.X, environment) {
			return nil, false
		}
	case "Library":
		if len(call.Args) == 0 {
			hostCall, ok := selector.X.(*ast.CallExpr)
			if ok {
				hostSelector, ok := hostCall.Fun.(*ast.SelectorExpr)
				if ok && hostSelector.Sel.Name == "Host" && w.evaluateBootstrap(pkg, hostSelector.X, environment) {
					return []string{""}, true
				}
			}
		}
	case "Namespace":
		if len(call.Args) == 1 {
			parents, ok := w.evaluateNamespace(pkg, selector.X, environment)
			name, nameOK := constantString(pkg, call.Args[0])

			if ok && nameOK {
				namespaces := make([]string, 0, len(parents))

				for _, parent := range parents {
					if parent == "" {
						namespaces = append(namespaces, name)
					} else {
						namespaces = append(namespaces, parent+"::"+name)
					}
				}

				return uniqueStrings(namespaces), true
			}
		}
	}

	return nil, false
}

func (w *walker) evaluateBootstrap(pkg *packages.Package, expression ast.Expr, environment environment) bool {
	identifier, ok := unwrapParens(expression).(*ast.Ident)
	if !ok {
		return false
	}

	return environment[pkg.TypesInfo.ObjectOf(identifier)].bootstrap
}

func (w *walker) evaluateDefinitions(pkg *packages.Package, expression ast.Expr, environment environment, active map[*types.Func]bool) ([]functionDefinition, bool) {
	expression = unwrapParens(expression)

	switch value := expression.(type) {
	case *ast.Ident:
		object := pkg.TypesInfo.ObjectOf(value)

		if definitions := environment[object].definitions; definitions != nil {
			return definitions, true
		}

		if declaration, exists := w.source.values[object]; exists {
			return w.evaluateDefinitions(declaration.pkg, declaration.expr, environment, active)
		}
	case *ast.CompositeLit:
		if !w.registrationValueType(pkg.TypesInfo.TypeOf(value)) {
			return nil, false
		}

		definitions := make([]functionDefinition, 0, len(value.Elts))

		for _, expression := range value.Elts {
			items, ok := w.evaluateDefinitions(pkg, expression, environment, active)
			if !ok {
				return nil, false
			}

			definitions = append(definitions, items...)
		}

		return definitions, true
	case *ast.CallExpr:
		if isPackageFunction(pkg, value.Fun, sdkPackagePath, "Func") {
			if len(value.Args) != 2 {
				return nil, false
			}

			name, ok := constantString(pkg, value.Args[0])
			if !ok || strings.TrimSpace(name) == "" {
				return nil, false
			}
			name = strings.TrimSpace(name)

			return []functionDefinition{{pkg: pkg, name: name, function: value.Args[1], position: value.Pos()}}, true
		}
		if identifier, ok := value.Fun.(*ast.Ident); ok && identifier.Name == "append" && pkg.TypesInfo.ObjectOf(identifier) == types.Universe.Lookup("append") {
			definitions := make([]functionDefinition, 0)

			for _, argument := range value.Args {
				items, ok := w.evaluateDefinitions(pkg, argument, environment, active)
				if !ok {
					return nil, false
				}

				definitions = append(definitions, items...)
			}

			return definitions, true
		}

		function := referencedFunction(pkg, value.Fun)
		if function == nil || function.Pkg() == nil || !w.localPackage(function.Pkg().Path()) || active[function] {
			return nil, false
		}

		node, exists := w.source.functions[function]
		if !exists {
			return nil, false
		}

		arguments := make([]abstractValue, len(value.Args))
		for index, argument := range value.Args {
			arguments[index] = w.evaluateValue(pkg, argument, environment)
		}

		active[function] = true
		defer delete(active, function)

		return w.returnedDefinitions(node, arguments, active)
	}

	return nil, false
}

func (w *walker) returnedDefinitions(node functionNode, arguments []abstractValue, active map[*types.Func]bool) ([]functionDefinition, bool) {
	env := make(environment)
	bindFieldList(node.pkg, node.decl.Type.Params, arguments, env)

	var definitions []functionDefinition
	resolved := false

	for _, statement := range node.decl.Body.List {
		switch value := statement.(type) {
		case *ast.AssignStmt:
			w.bindAssignment(node.pkg, value.Lhs, value.Rhs, env)
		case *ast.DeclStmt:
			if general, ok := value.Decl.(*ast.GenDecl); ok {
				for _, specification := range general.Specs {
					if declaration, ok := specification.(*ast.ValueSpec); ok {
						w.bindNames(node.pkg, declaration.Names, declaration.Values, env)
					}
				}
			}
		case *ast.ReturnStmt:
			if len(value.Results) != 1 {
				return nil, false
			}

			items, ok := w.evaluateDefinitions(node.pkg, value.Results[0], env, active)
			if !ok {
				return nil, false
			}

			definitions = append(definitions, items...)
			resolved = true
		case *ast.IfStmt:
			return nil, false
		default:
			return nil, false
		}
	}

	return definitions, resolved
}

func (w *walker) containsRegistration(pkg *packages.Package, node ast.Node, environment environment) bool {
	found := false

	ast.Inspect(node, func(current ast.Node) bool {
		if found {
			return false
		}

		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}

		if isPackageFunction(pkg, call.Fun, sdkPackagePath, "RegisterFunctions") {
			found = true

			return false
		}

		if isRuntimeFunctionBuilderMethod(pkg, call.Fun, "Add", "From", "Remove") {
			found = true

			return false
		}

		for _, argument := range call.Args {
			value := w.evaluateValue(pkg, argument, environment)
			if value.bootstrap || value.namespaces != nil || value.definitions != nil || value.unknown {
				found = true

				return false
			}
		}

		return true
	})

	return found
}

func (w *walker) containsRegistrationMutation(pkg *packages.Package, node ast.Node, environment environment) bool {
	found := false

	ast.Inspect(node, func(current ast.Node) bool {
		if found {
			return false
		}

		if _, nested := current.(*ast.FuncLit); nested {
			return false
		}

		switch value := current.(type) {
		case *ast.CallExpr:
			if isPackageFunction(pkg, value.Fun, sdkPackagePath, "Func") {
				found = true

				return false
			}

			if function := referencedFunction(pkg, value.Fun); function != nil && function.Pkg() != nil &&
				function.Pkg().Path() == runtimePackagePath && function.Name() == "Namespace" {
				found = true

				return false
			}
		case *ast.AssignStmt:
			for _, expression := range value.Lhs {
				if identifier, ok := expression.(*ast.Ident); ok {
					if tracked, exists := environment[pkg.TypesInfo.ObjectOf(identifier)]; exists &&
						(tracked.bootstrap || tracked.namespaces != nil || tracked.definitions != nil || tracked.unknown) {
						found = true

						return false
					}
				}
			}

			for _, expression := range value.Rhs {
				if w.registrationValueType(pkg.TypesInfo.TypeOf(expression)) {
					found = true

					return false
				}
			}
		case *ast.ValueSpec:
			for _, expression := range value.Values {
				if w.registrationValueType(pkg.TypesInfo.TypeOf(expression)) {
					found = true

					return false
				}
			}
		}

		return true
	})

	return found
}

func mergeAbstractValues(values ...abstractValue) abstractValue {
	merged := abstractValue{}

	for _, value := range values {
		merged.bootstrap = merged.bootstrap || value.bootstrap
		merged.unknown = merged.unknown || value.unknown

		if value.namespaces != nil {
			merged.namespaces = append(merged.namespaces, value.namespaces...)
		}

		if value.definitions != nil {
			if merged.definitions == nil {
				merged.definitions = make([]functionDefinition, 0, len(value.definitions))
			}

			merged.definitions = append(merged.definitions, value.definitions...)
		}
	}

	if merged.namespaces != nil {
		merged.namespaces = uniqueStrings(merged.namespaces)
	}

	return merged
}

func uniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))

	for _, value := range values {
		if _, exists := unique[value]; exists {
			continue
		}

		unique[value] = struct{}{}
		result = append(result, value)
	}

	return result
}

func mergeEnvironmentBranches(target environment, branches ...environment) {
	for object := range target {
		values := make([]abstractValue, 0, len(branches))

		for _, branch := range branches {
			values = append(values, branch[object])
		}

		target[object] = mergeAbstractValues(values...)
	}
}

func (w *walker) localPackage(path string) bool {
	return path == w.source.modulePath || strings.HasPrefix(path, w.source.modulePath+"/")
}

func (w *walker) registrationValueType(value types.Type) bool {
	if value == nil {
		return false
	}

	value = types.Unalias(value)

	if pointer, ok := value.(*types.Pointer); ok {
		return w.registrationValueType(pointer.Elem())
	}

	if named, ok := value.(*types.Named); ok {
		if object := named.Obj(); object.Pkg() != nil {
			switch object.Pkg().Path() {
			case modulePackagePath:
				if object.Name() == "Bootstrap" || object.Name() == "HostContext" {
					return true
				}
			case runtimePackagePath:
				switch object.Name() {
				case "Namespace", "Library", "FunctionDefs", "FnDef", "FunctionsBuilder":
					return true
				}
			case sdkPackagePath:
				if object.Name() == "FunctionDef" {
					return true
				}
			}
		}

		value = named.Underlying()
	}

	switch aggregate := value.(type) {
	case *types.Slice:
		return w.registrationValueType(aggregate.Elem())
	case *types.Array:
		return w.registrationValueType(aggregate.Elem())
	}
	return false
}
