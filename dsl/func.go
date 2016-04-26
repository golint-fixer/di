package dsl

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	log "github.com/Sirupsen/logrus"
)

type funcImpl struct {
	do      func(*evalCtx, []ast) (ast, error)
	minArgs int
	lazy    bool // True if arguments shoudl not be evaluated automatically.
}

var funcImplMap map[astIdent]funcImpl

// We have to initialize `funcImplMap` in an init function or else the compiler
// will complain about an initialization loop (funcImplMap -> letImpl ->
// astSexp.eval -> funcImplMap).
func init() {
	mod := arithFun(func(a, b int) int { return a % b })
	mul := arithFun(func(a, b int) int { return a * b })
	sub := arithFun(func(a, b int) int { return a - b })
	div := arithFun(func(a, b int) int { return a / b })

	less := compareFun(func(a, b int) bool { return a < b })
	more := compareFun(func(a, b int) bool { return a > b })

	funcImplMap = map[astIdent]funcImpl{
		"!":                {notImpl, 1, false},
		"%":                {mod, 2, false},
		"*":                {mul, 2, false},
		"+":                {plusImpl, 2, false},
		"-":                {sub, 2, false},
		"/":                {div, 2, false},
		"<":                {less, 2, false},
		"=":                {eqImpl, 2, false},
		">":                {more, 2, false},
		"and":              {andImpl, 1, true},
		"apply":            {applyImpl, 2, false},
		"bool":             {boolImpl, 1, false},
		"car":              {carImpl, 1, false},
		"cdr":              {cdrImpl, 1, false},
		"connect":          {connectImpl, 3, false},
		"cons":             {consImpl, 2, false},
		"cpu":              {rangeTypeImpl("cpu"), 1, false},
		"define":           {defineImpl, 2, true},
		"docker":           {dockerImpl, 1, false},
		"setEnv":           {setEnvImpl, 3, false},
		"githubKey":        {githubKeyImpl, 1, false},
		"hmap":             {hmapImpl, 0, true},
		"hmapGet":          {hmapGetImpl, 2, false},
		"hmapContains":     {hmapContainsImpl, 2, false},
		"hmapSet":          {hmapSetImpl, 3, false},
		"if":               {ifImpl, 2, true},
		"import":           {importImpl, 1, true},
		"label":            {labelImpl, 2, false},
		"labelName":        {labelNameImpl, 1, false},
		"lambda":           {lambdaImpl, 2, true},
		"let":              {letImpl, 1, true},
		"len":              {lenImpl, 1, false},
		"log":              {logImpl, 2, false},
		"list":             {listImpl, 0, false},
		"machine":          {machineImpl, 0, false},
		"machineAttribute": {machineAttributeImpl, 2, false},
		"makeList":         {makeListImpl, 2, true},
		"map":              {mapImpl, 2, false},
		"module":           {moduleImpl, 2, true},
		"nth":              {nthImpl, 2, false},
		"or":               {orImpl, 1, true},
		"placement":        {placementImpl, 2, false},
		"plaintextKey":     {plaintextKeyImpl, 1, false},
		"panic":            {panicImpl, 1, false},
		"progn":            {prognImpl, 1, false},
		"provider":         {providerImpl, 1, false},
		"reduce":           {reduceImpl, 2, false},
		"ram":              {rangeTypeImpl("ram"), 1, false},
		"range":            {rangeImpl, 1, false},
		"role":             {roleImpl, 1, false},
		"size":             {sizeImpl, 1, false},
		"sprintf":          {sprintfImpl, 1, false},
	}
}

// XXX: support float operators?
func arithFun(do func(a, b int) int) func(*evalCtx, []ast) (ast, error) {
	return func(ctx *evalCtx, args []ast) (ast, error) {
		var ints []int
		for _, arg := range args {
			ival, ok := arg.(astInt)
			if !ok {
				err := fmt.Errorf("bad arithmetic argument: %s", arg)
				return nil, err
			}
			ints = append(ints, int(ival))
		}

		total := ints[0]
		for _, x := range ints[1:] {
			total = do(total, x)
		}

		return astInt(total), nil
	}
}

func plusImpl(ctx *evalCtx, args []ast) (ast, error) {
	if _, ok := args[0].(astString); !ok {
		return arithFun(func(a, b int) int { return a + b })(ctx, args)
	}

	var strSlice []string
	for _, arg := range args {
		str, ok := arg.(astString)
		if !ok {
			err := fmt.Errorf("bad string concatenation argument: %s", arg)
			return nil, err
		}
		strSlice = append(strSlice, string(str))
	}

	return astString(strings.Join(strSlice, "")), nil
}

func eqImpl(ctx *evalCtx, args []ast) (ast, error) {
	return astBool(reflect.DeepEqual(args[0], args[1])), nil
}

func compareFun(do func(a, b int) bool) func(*evalCtx, []ast) (ast, error) {
	return func(ctx *evalCtx, args []ast) (ast, error) {
		var ints []int
		for _, arg := range args {
			ival, ok := arg.(astInt)
			if !ok {
				err := fmt.Errorf("bad arithmetic argument: %s", arg)
				return nil, err
			}
			ints = append(ints, int(ival))
		}

		return astBool(do(ints[0], ints[1])), nil
	}
}

func dockerImpl(ctx *evalCtx, evalArgs []ast) (ast, error) {
	args, err := flattenString(evalArgs)
	if err != nil {
		return nil, err
	}

	var astArgs []ast
	for _, arg := range args {
		astArgs = append(astArgs, astString(arg))
	}

	newContainer := &astContainer{
		image:     astArgs[0].(astString),
		command:   astList(astArgs[1:]),
		Placement: Placement{make(map[[2]string]struct{})},
		env:       astHmap(make(map[ast]ast)),
	}

	globalCtx := ctx.globalCtx()
	*globalCtx.containers = append(*globalCtx.containers, newContainer)

	return newContainer, nil
}

func setEnvImpl(ctx *evalCtx, args []ast) (ast, error) {
	for _, arg := range flatten([]ast{args[0]}) {
		switch val := arg.(type) {
		case astString:
			labels, err := ctx.flattenLabel([]ast{val})
			if err != nil {
				return nil, err
			}
			for _, label := range labels {
				for _, c := range label.elems {
					c, ok := c.(*astContainer)
					if !ok {
						return nil, fmt.Errorf("environment variables require labels that contain containers: %s", val)
					}
					c.env[args[1]] = args[2]
				}
			}
		default:
			c, ok := val.(*astContainer)
			if !ok {
				return nil, fmt.Errorf("unrecognized type to set environment variables: %s", arg)
			}
			c.env[args[1]] = args[2]
		}
	}
	return astList(make([]ast, 0)), nil
}

func githubKeyImpl(ctx *evalCtx, args []ast) (ast, error) {
	username, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("github username must be a string: %s", args[0])
	}
	return astGithubKey(username), nil
}

func plaintextKeyImpl(ctx *evalCtx, args []ast) (ast, error) {
	key, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("key must be a string: %s", args[0])
	}
	return astPlaintextKey(key), nil
}

func placementImpl(ctx *evalCtx, args []ast) (ast, error) {
	str, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("placement type must be a string, found: %s", args[0])
	}
	ptype := string(str)

	labels, err := ctx.flattenLabel(args[1:])
	if err != nil {
		return nil, err
	}

	if len(labels) < 2 {
		return nil, fmt.Errorf("placment requires at least 2 labels.")
	}

	parsedLabels := make(map[[2]string]struct{})
	for i := 0; i < len(labels)-1; i++ {
		labelNameI := string(labels[i].ident)
		for j := i + 1; j < len(labels); j++ {
			labelNameJ := string(labels[j].ident)
			if labelNameI < labelNameJ {
				parsedLabels[[2]string{labelNameI, labelNameJ}] = struct{}{}
			} else {
				parsedLabels[[2]string{labelNameJ, labelNameI}] = struct{}{}
			}
		}
	}

	switch ptype {
	case "exclusive":
		for _, label := range labels {
			for _, c := range label.elems {
				c, ok := c.(*astContainer)
				if !ok {
					return nil, fmt.Errorf("placement labels must contain containers: %s", label)
				}
				for k, v := range parsedLabels {
					c.Placement.Exclusive[k] = v
				}
			}
		}
	default:
		return nil, fmt.Errorf("not a valid placement type: %s", ptype)
	}

	return astFunc(astIdent("placement"), args), nil
}

func setMachineAttributes(machine *astMachine, args []ast) error {
	for _, arg := range flatten(args) {
		switch val := arg.(type) {
		case astProvider:
			machine.provider = val
		case astSize:
			machine.size = val
		case astRole:
			machine.role = val
		case astRange:
			switch string(val.ident) {
			case "ram":
				machine.ram = val
			case "cpu":
				machine.cpu = val
			default:
				return fmt.Errorf("unrecognized argument to machine definition: %s", arg)
			}
		case key:
			machine.sshKeys = append(machine.sshKeys, val)
		default:
			return fmt.Errorf("unrecognized argument to machine definition: %s", arg)
		}
	}
	return nil
}

func machineImpl(ctx *evalCtx, args []ast) (ast, error) {
	machine := &astMachine{}
	err := setMachineAttributes(machine, args)
	if err != nil {
		return nil, err
	}

	globalCtx := ctx.globalCtx()
	*globalCtx.machines = append(*globalCtx.machines, machine)

	return machine, nil
}

func machineAttributeImpl(ctx *evalCtx, args []ast) (ast, error) {
	list := flatten([]ast{args[0]})

	var processedMachines []ast
	for _, m := range list {
		machine, ok := m.(*astMachine)
		if !ok {
			return nil, fmt.Errorf("bad type, cannot change machine attributes: %s", m)
		}
		err := setMachineAttributes(machine, args[1:])
		if err != nil {
			return nil, err
		}
		processedMachines = append(processedMachines, machine)
	}

	return astList(processedMachines), nil
}

func providerImpl(ctx *evalCtx, args []ast) (ast, error) {
	return astProvider((args[0].(astString))), nil
}

func roleImpl(ctx *evalCtx, args []ast) (ast, error) {
	role, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("role must be a string: %s", args[0])
	}
	return astRole(role), nil
}

func sizeImpl(ctx *evalCtx, args []ast) (ast, error) {
	return astSize((args[0].(astString))), nil
}

func toFloat(x ast) (astFloat, error) {
	switch x.(type) {
	case astInt:
		return astFloat(x.(astInt)), nil
	case astFloat:
		return x.(astFloat), nil
	default:
		return astFloat(0), fmt.Errorf("%v is not convertable to a float", x)
	}
}

func rangeTypeImpl(rangeType string) func(*evalCtx, []ast) (ast, error) {
	return func(ctx *evalCtx, args []ast) (ast, error) {
		var max astFloat
		var maxErr error
		if len(args) > 1 {
			max, maxErr = toFloat(args[1])
		}
		min, minErr := toFloat(args[0])

		if minErr != nil || maxErr != nil {
			return nil, fmt.Errorf("range arguments must be convertable to floats: %v", args)
		}

		return astRange{ident: astIdent(rangeType), min: min, max: max}, nil
	}
}

func connectImpl(ctx *evalCtx, args []ast) (ast, error) {
	var min, max int
	switch t := args[0].(type) {
	case astInt:
		min, max = int(t), int(t)
	case astList:
		if len(t) != 2 {
			return nil, fmt.Errorf("port range must have two ints: %s", t)
		}

		minAst, minOK := t[0].(astInt)
		maxAst, maxOK := t[1].(astInt)
		if !minOK || !maxOK {
			return nil, fmt.Errorf("port range must have two ints: %s", t)
		}

		min, max = int(minAst), int(maxAst)
	default:
		return nil, fmt.Errorf("port range must be an int or a list of ints:"+
			" %s", args[0])
	}

	if min < 0 || max > 65535 {
		return nil, fmt.Errorf("invalid port range: [%d, %d]", min, max)
	}

	if min > max {
		return nil, fmt.Errorf("invalid port range: [%d, %d]", min, max)
	}

	fromLabels, err := ctx.flattenLabel([]ast{args[1]})
	if err != nil {
		return nil, err
	}

	toLabels, err := ctx.flattenLabel(args[2:])
	if err != nil {
		return nil, err
	}

	for _, from := range fromLabels {
		for _, to := range toLabels {
			cn := Connection{
				From:    string(from.ident),
				To:      string(to.ident),
				MinPort: min,
				MaxPort: max,
			}
			ctx.globalCtx().connections[cn] = struct{}{}
		}
	}

	return astList{}, nil
}

func labelImpl(ctx *evalCtx, args []ast) (ast, error) {
	str, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("label must be a string, found: %s", args[0])
	}
	label := string(str)
	if label != strings.ToLower(label) {
		log.Error("Labels must be lowercase, sorry! https://github.com/docker/swarm/issues/1795")
	}

	if _, ok := ctx.resolveLabel(str); ok {
		return nil, fmt.Errorf("attempt to redefine label: %s", label)
	}

	var atoms []atom
	for _, elem := range flatten(args[1:]) {
		switch t := elem.(type) {
		case atom:
			atoms = append(atoms, t)
		case astString, astLabel:
			l, ok := ctx.resolveLabel(t)
			if !ok {
				return nil, fmt.Errorf("undefined label: %s", t)
			}

			for _, c := range l.elems {
				atoms = append(atoms, c)
			}
		default:
			return nil, fmt.Errorf("label must apply to atoms or other"+
				" labels, found: %s", elem)
		}
	}

	for _, a := range atoms {
		labels := a.Labels()
		if len(labels) > 0 && labels[len(labels)-1] == label {
			// It's possible that the same container appears in the list
			// twice.  If that's the case, we'll end up labelling it multiple
			// times unless we check it's most recently added label.
			continue
		}

		a.SetLabels(append(labels, label))
	}

	newLabel := astLabel{ident: astString(label), elems: atoms}
	ctx.globalCtx().labels[label] = newLabel

	return newLabel, nil
}

func labelNameImpl(ctx *evalCtx, args []ast) (ast, error) {
	label, ok := ctx.resolveLabel(args[0])
	if !ok {
		return nil, fmt.Errorf("labelName applies to labels: %s", args[0])
	}
	return label.ident, nil
}

func listImpl(ctx *evalCtx, args []ast) (ast, error) {
	return astList(args), nil
}

func makeListImpl(ctx *evalCtx, args []ast) (ast, error) {
	eval, err := args[0].eval(ctx)
	if err != nil {
		return nil, err
	}

	count, ok := eval.(astInt)
	if !ok || count < 0 {
		return nil, fmt.Errorf("makeList must begin with a positive integer, "+
			"found: %s", args[0])
	}

	var result []ast
	for i := 0; i < int(count); i++ {
		eval, err := args[1].eval(ctx)
		if err != nil {
			return nil, err
		}
		result = append(result, eval)
	}
	return astList(result), nil
}

func hmapImpl(ctx *evalCtx, args []ast) (ast, error) {
	m := astHmap(make(map[ast]ast))
	bindings, err := parseBindings(ctx, astSexp{sexp: args})
	if err != nil {
		return nil, err
	}
	for _, bind := range bindings {
		key, err := bind.key.eval(ctx)
		if err != nil {
			return nil, err
		}
		val, err := bind.val.eval(ctx)
		if err != nil {
			return nil, err
		}
		m[key] = val
	}
	return m, nil
}

func hmapSetImpl(ctx *evalCtx, args []ast) (ast, error) {
	m, ok := args[0].(astHmap)
	if !ok {
		return nil, fmt.Errorf("%s must be a hmap", args[0])
	}
	newM := astHmap(make(map[ast]ast))
	for k, v := range m {
		newM[k] = v
	}
	newM[args[1]] = args[2]
	return newM, nil
}

func hmapGetImpl(ctx *evalCtx, args []ast) (ast, error) {
	m, ok := args[0].(astHmap)
	if !ok {
		return nil, fmt.Errorf("%s must be a hmap", args[0])
	}
	value, ok := m[args[1]]
	if !ok {
		return nil, fmt.Errorf("undefined key: %s", args[1])
	}
	return value, nil
}

func hmapContainsImpl(ctx *evalCtx, args []ast) (ast, error) {
	m, ok := args[0].(astHmap)
	if !ok {
		return nil, fmt.Errorf("%s must be a hmap", args[0])
	}
	_, ok = m[args[1]]
	return astBool(ok), nil
}

func sprintfImpl(ctx *evalCtx, args []ast) (ast, error) {
	format, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("sprintf format must be a string: %s", args[0])
	}

	var ifaceArgs []interface{}
	for _, arg := range args[1:] {
		var iface interface{}
		switch t := arg.(type) {
		case astString:
			iface = string(t)
		case astInt:
			iface = int(t)
		default:
			iface = t
		}

		ifaceArgs = append(ifaceArgs, iface)
	}

	return astString(fmt.Sprintf(string(format), ifaceArgs...)), nil
}

func logImpl(ctx *evalCtx, args []ast) (ast, error) {
	level, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("log level must be a string: %s\n", level)
	}

	msgAst, ok := args[1].(astString)
	if !ok {
		return nil, fmt.Errorf("log message must be a string: %s\n", args[1])
	}

	msg := string(msgAst)
	switch level {
	case "print":
		log.Println(msg)
	case "debug":
		log.Debugln(msg)
	case "info":
		log.Infoln(msg)
	case "warn":
		log.Warnln(msg)
	case "error":
		log.Errorln(msg)
	default:
		return nil, fmt.Errorf("log level must be one of [print, info, debug, warn, error]: %s", level)
	}

	return astList{}, nil
}

func panicImpl(ctx *evalCtx, args []ast) (ast, error) {
	msg, ok := args[0].(astString)
	if !ok {
		return nil, fmt.Errorf("panic message must be a string: %s\n", args[1])
	}

	return nil, fmt.Errorf("panic: runtime error: %s", string(msg))
}

func defineImpl(ctx *evalCtx, args []ast) (ast, error) {
	var ident astIdent
	var value ast
	switch t := args[0].(type) {
	case astSexp:
		if len(t.sexp) < 1 {
			return nil, errors.New("define function must begin with a name")
		}

		var ok bool
		ident, ok = t.sexp[0].(astIdent)
		if !ok {
			return nil, fmt.Errorf("define name must be an ident: %v",
				t.sexp[0])
		}

		var err error
		binds := astSexp{sexp: t.sexp[1:]}
		value, err = lambdaImpl(ctx, append([]ast{binds}, args[1:]...))
		if err != nil {
			return nil, err
		}
	case astIdent:
		if len(args) > 2 {
			return nil, fmt.Errorf("not enough arguments: %s", t)
		}

		ident = t

		var err error
		value, err = args[1].eval(ctx)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("define name must be an ident: %v", args[0])
	}

	if _, ok := ctx.binds[ident]; ok {
		return nil, fmt.Errorf("attempt to redefine: \"%s\"", ident)
	}

	ctx.binds[ident] = value

	return astList{}, nil
}

type binding struct {
	key ast
	val ast
}

func parseBindings(ctx *evalCtx, bindings astSexp) ([]binding, error) {
	var binds []binding
	for _, astBinding := range bindings.sexp {
		bind, ok := astBinding.(astSexp)
		if !ok || len(bind.sexp) != 2 {
			return nil, fmt.Errorf("binds must be exactly 2 arguments: %s", astBinding)
		}
		binds = append(binds, binding{bind.sexp[0], bind.sexp[1]})
	}
	return binds, nil
}

func lambdaImpl(ctx *evalCtx, args []ast) (ast, error) {
	rawArgNames, ok := args[0].(astSexp)
	if !ok {
		return nil, fmt.Errorf("lambda functions must define an argument list")
	}

	var argNames []astIdent
	for _, argName := range rawArgNames.sexp {
		ident, ok := argName.(astIdent)
		if !ok {
			return nil, fmt.Errorf("lambda argument names must be idents")
		}
		argNames = append(argNames, ident)
	}
	return astLambda{argNames: argNames, do: args[1:], ctx: ctx.deepCopy()}, nil
}

func letImpl(ctx *evalCtx, args []ast) (ast, error) {
	bindingsRaw, ok := args[0].(astSexp)
	if !ok {
		return nil, fmt.Errorf("let binds must be defined in an S-expression")
	}

	bindings, err := parseBindings(ctx, bindingsRaw)
	if err != nil {
		return nil, err
	}

	bindCtx := newEvalCtx(ctx)

	var names []astIdent
	var vals []ast
	for _, pair := range bindings {
		key, ok := pair.key.(astIdent)
		if !ok {
			return nil, fmt.Errorf("bind name must be an ident: %s", pair.key)
		}
		val, err := pair.val.eval(&bindCtx)
		if err != nil {
			return nil, err
		}

		// Our let is similar to let* in other lisps.  The result of earlier
		// bindings are available to later ones.
		bindCtx.binds[key] = val

		names = append(names, key)
		vals = append(vals, val)
	}

	// A let without a body has nil as an implicit body.
	body := []ast{astList{}}
	if len(args) > 1 {
		body = args[1:]
	}
	lambda := astLambda{argNames: names, do: body, ctx: ctx}
	let := astSexp{sexp: append([]ast{lambda}, vals...)}
	return let.eval(ctx)
}

func boolImpl(ctx *evalCtx, args []ast) (ast, error) {
	return toBool(args[0]), nil
}

// An ast element is false if it's the empty list, empty string, 0, or false,
// and true otherwise.
func toBool(arg ast) astBool {
	switch val := arg.(type) {
	case astList:
		return astBool(len(val) != 0)
	case astString:
		return astBool(len(val) != 0)
	case astInt:
		return astBool(val != 0)
	case astBool:
		return val
	default:
		return astBool(true)
	}
}

func ifImpl(ctx *evalCtx, args []ast) (ast, error) {
	predAst, err := args[0].eval(ctx)
	if err != nil {
		return nil, err
	}

	if toBool(predAst) {
		return args[1].eval(ctx)
	}

	// If the predicate is false, but there's no else case.
	if len(args) == 2 {
		return astList{}, nil
	}
	return args[2].eval(ctx)
}

func andImpl(ctx *evalCtx, args []ast) (ast, error) {
	for _, arg := range args {
		predAst, err := arg.eval(ctx)
		if err != nil {
			return nil, err
		}

		if !toBool(predAst) {
			return astBool(false), nil
		}
	}
	return astBool(true), nil
}

func orImpl(ctx *evalCtx, args []ast) (ast, error) {
	for _, arg := range args {
		predAst, err := arg.eval(ctx)
		if err != nil {
			return nil, err
		}

		if toBool(predAst) {
			return astBool(true), nil
		}
	}
	return astBool(false), nil
}

func notImpl(ctx *evalCtx, args []ast) (ast, error) {
	predAst, err := args[0].eval(ctx)
	if err != nil {
		return nil, err
	}

	return !toBool(predAst), nil
}

func applyImpl(ctx *evalCtx, args []ast) (ast, error) {
	list, ok := args[1].(astList)
	if !ok {
		return nil, fmt.Errorf("apply requires a lists: %s", args[1])
	}

	return astSexp{sexp: append(args[0:1], list...)}.eval(ctx)
}

func shouldExport(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func importImpl(ctx *evalCtx, args []ast) (ast, error) {
	return nil, errors.New("import must be begin the module")
}

func moduleImpl(ctx *evalCtx, args []ast) (ast, error) {
	moduleName, err := args[0].eval(ctx)
	if err != nil {
		return nil, err
	}

	moduleNameStr, ok := moduleName.(astString)
	if !ok {
		return nil, fmt.Errorf("module name must be a string: %s", moduleName)
	}

	return astModule{moduleName: moduleNameStr, body: astRoot(args[1:])}.eval(ctx)
}

func prognImpl(ctx *evalCtx, args []ast) (ast, error) {
	return args[len(args)-1], nil
}

func lenImpl(ctx *evalCtx, args []ast) (ast, error) {
	list, ok := args[0].(astList)
	if !ok {
		return nil, fmt.Errorf("len applies to lists: %s", args[0])
	}

	return astInt(len(list)), nil
}

func carImpl(ctx *evalCtx, args []ast) (ast, error) {
	list, ok := args[0].(astList)
	if !ok || len(list) <= 0 {
		return nil, fmt.Errorf("car applies to populated lists: %s", args[0])
	}

	return list[0], nil
}

func cdrImpl(ctx *evalCtx, args []ast) (ast, error) {
	list, ok := args[0].(astList)
	if !ok || len(list) <= 0 {
		return nil, fmt.Errorf("cdr applies to populated lists: %s", args[0])
	}

	return astList(list[1:]), nil
}

func consImpl(ctx *evalCtx, args []ast) (ast, error) {
	list, ok := args[1].(astList)
	if !ok {
		return nil, fmt.Errorf("cons applies to lists: %s", args[0])
	}

	return astList(append([]ast{args[0]}, list...)), nil
}

func nthImpl(ctx *evalCtx, args []ast) (ast, error) {
	index, ok := args[0].(astInt)
	if !ok {
		return nil, fmt.Errorf("nth list index must be an int: %s", args[0])
	}

	list, ok := args[1].(astList)
	if !ok {
		return nil, fmt.Errorf("nth applies to lists: %s", args[1])
	}

	if int(index) < 0 || int(index) >= len(list) {
		return nil, fmt.Errorf("array index out of bounds: %d", index)
	}

	return list[index], nil
}

func mapImpl(ctx *evalCtx, args []ast) (ast, error) {
	var lists [][]ast
	for _, ast := range args[1:] {
		list, ok := ast.(astList)
		if !ok {
			return nil, fmt.Errorf("map applies to list: %s", ast)
		}

		lists = append(lists, list)
	}

	listLen := len(lists[0])
	for _, list := range lists[1:] {
		if len(list) != listLen {
			return nil, errors.New("unbalanced lists")
		}
	}

	var mapped astList
	for i := 0; i < listLen; i++ {
		sexp := []ast{args[0]}
		for _, list := range lists {
			sexp = append(sexp, list[i])
		}

		elem, err := astSexp{sexp: sexp}.eval(ctx)
		if err != nil {
			return nil, err
		}

		mapped = append(mapped, elem)
	}

	return mapped, nil
}

func reduceImpl(ctx *evalCtx, args []ast) (ast, error) {
	list, ok := args[1].(astList)
	if !ok {
		return nil, fmt.Errorf("reduce applies to lists: %s", args[1])
	}

	if len(list) < 2 {
		return nil, fmt.Errorf("not enough elements to reduce: %s", list)
	}

	res := list[0]
	for _, item := range list[1:] {
		var err error
		res, err = astSexp{sexp: []ast{args[0], res, item}}.eval(ctx)
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

// `range` operates like the range function in python.  If there's one argument, it
// counts from 1 to n, if there's to, the first argument is considered the start, and the
// second is considered the stop, and if there's three then the third argument is
// considered a steparator.
func rangeImpl(ctx *evalCtx, args []ast) (ast, error) {
	for _, arg := range args {
		if _, ok := arg.(astInt); !ok {
			err := fmt.Errorf("range arguments must be integers: %v", arg)
			return nil, err
		}
	}

	var start, stop, step int
	switch len(args) {
	case 1:
		start = 0
		stop = int(args[0].(astInt))
		step = 1
	case 2:
		start = int(args[0].(astInt))
		stop = int(args[1].(astInt))
		step = 1
	case 3:
		start = int(args[0].(astInt))
		stop = int(args[1].(astInt))
		step = int(args[2].(astInt))
	default:
		return nil, fmt.Errorf("range expects 1, 2 or 3 arguments, found: %d",
			len(args))
	}

	if step <= 0 {
		return nil, fmt.Errorf("step must be greater than zero")
	}

	var asts astList
	for i := start; i < stop; i += step {
		asts = append(asts, astInt(i))
	}

	return asts, nil
}

func flatten(lst []ast) []ast {
	var result []ast

	for _, l := range lst {
		switch t := l.(type) {
		case astList:
			result = append(result, flatten(t)...)
		default:
			result = append(result, l)
		}
	}

	return result
}

func flattenString(lst []ast) ([]string, error) {
	var strings []string
	for _, elem := range flatten(lst) {
		str, ok := elem.(astString)
		if !ok {
			return nil, fmt.Errorf("expected string, found: %v", elem)
		}
		strings = append(strings, string(str))
	}

	return strings, nil
}

func (ctx evalCtx) flattenLabel(lst []ast) ([]astLabel, error) {
	var labels []astLabel
	for _, elem := range flatten(lst) {
		label, ok := ctx.resolveLabel(elem)
		if !ok {
			return nil, fmt.Errorf("expected label, found: %v", elem)
		}
		labels = append(labels, label)
	}

	return labels, nil
}

func astFunc(ident astIdent, args []ast) astSexp {
	return astSexp{sexp: append([]ast{ident}, args...)}
}

func (ctx evalCtx) resolveLabel(labelRef ast) (astLabel, bool) {
	switch val := labelRef.(type) {
	case astString:
		if label, ok := ctx.labels[string(val)]; ok {
			return label, true
		}
	case astLabel:
		return val, true
	}
	if ctx.parent != nil {
		return ctx.parent.resolveLabel(labelRef)
	}
	return astLabel{}, false
}
