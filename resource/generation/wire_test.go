package generation

import (
	"go/format"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
)

// walkFixture walks one wirefixture struct the way extraction does.
func walkFixture(t *testing.T, structs map[string]*parser.Struct, name string) (*wireShape, error) {
	t.Helper()

	s := structs[name]
	if s == nil {
		t.Fatalf("struct %q not in fixture", name)
	}

	return newWireWalker(defaultTypescriptOverrides(), "wirefixture", "resources").walk(s)
}

func Test_wireWalker(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wirefixture"))

	tests := []struct {
		name       string
		structName string
		wantErr    string
		wantFlat   bool
		// wantNested lists the nested mirrors in declaration order, leaves first.
		wantNested []string
		// wantMirror maps a top-level field to its type inside the mirror.
		wantMirror map[string]string
		// wantTS maps a top-level field to its TypeScript type in the "Root" namespace.
		wantTS map[string]string
	}{
		{
			name: "flat struct converts whole", structName: "Flat", wantFlat: true,
			wantMirror: map[string]string{"ID": "ccc.UUID", "Tags": "[]string", "When": "*time.Time", "Code": "wirefixture.Code"},
			wantTS:     map[string]string{"ID": "string", "Tags": "string[]", "When": "Date", "Code": "string", "Count": "number"},
		},
		{
			name: "nested report declares mirrors leaves first", structName: "Report",
			wantNested: []string{"reading", "system", "ship"},
			wantMirror: map[string]string{"Ship": "ship", "Primary": "*reading", "Notes": "[]string"},
			wantTS:     map[string]string{"Ship": "Root.Ship", "Primary": "Root.Reading", "Notes": "string[]"},
		},
		{
			name: "a struct reached twice is mirrored once", structName: "Shared",
			wantNested: []string{"reading"},
			wantMirror: map[string]string{"First": "reading", "Second": "[]reading"},
			wantTS:     map[string]string{"First": "Root.Reading", "Second": "Root.Reading[]"},
		},
		{name: "recursion", structName: "Recursive", wantErr: "Recursive.Child: wirefixture.Recursive reaches itself"},
		{name: "mutual recursion", structName: "Mutual", wantErr: "Mutual.Other.Back: wirefixture.Mutual reaches itself"},
		{name: "map", structName: "WithMap", wantErr: "WithMap.M: a map does not cross the wire"},
		{name: "any", structName: "WithAny", wantErr: "WithAny.A: an interface does not cross the wire"},
		{name: "interface", structName: "WithInterface", wantErr: "WithInterface.I: fmt.Stringer is a named interface"},
		{name: "slice of slices", structName: "SliceOfSlices", wantErr: "SliceOfSlices.S: a slice of slices does not cross the wire"},
		{name: "pointer to slice", structName: "PointerToSlice", wantErr: "PointerToSlice.P: a pointer to a slice does not cross the wire"},
		{name: "embedded", structName: "Embedded", wantErr: "Embedded.Reading: embedded fields do not cross the wire"},
		{name: "unexported", structName: "Unexported", wantErr: "Unexported.hidden: unexported fields do not cross the wire"},
		{name: "anonymous struct", structName: "AnonStruct", wantErr: "AnonStruct.A: an anonymous struct does not cross the wire"},
		{name: "array", structName: "ArrayField", wantErr: "ArrayField.A: an array does not cross the wire"},
		{name: "named slice", structName: "NamedSlice", wantErr: "NamedSlice.IDs: wirefixture.UUIDs is a named []ccc.UUID"},
		{name: "two types one mirror name", structName: "Collision", wantErr: `Collision.B: other.Reading and wirefixture.Reading would both mirror as "reading"`},
		{name: "mirror shadows an imported package", structName: "ShadowsPackage", wantErr: `wirefixture.Time would mirror as "time", a package the generated handler imports`},
		{name: "mirror shadows a handler identifier", structName: "ShadowsHandler", wantErr: `wirefixture.Request would mirror as "request", a name the generated handler uses`},
		{name: "local shadows a converter identifier", structName: "LocalCollision", wantErr: `wirefixture.LocalCollision.View: the field's name "view" is used by the generated converter`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shape, err := walkFixture(t, structs, tt.structName)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("walk(%s) error = %v, want containing %q", tt.structName, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("walk(%s) error = %v", tt.structName, err)
			}
			if got := shape.Flat(); got != tt.wantFlat {
				t.Errorf("Flat() = %v, want %v", got, tt.wantFlat)
			}
			var nested []string
			for _, n := range shape.Nested() {
				nested = append(nested, n.Mirror)
			}
			if strings.Join(nested, ",") != strings.Join(tt.wantNested, ",") {
				t.Errorf("Nested() = %v, want %v", nested, tt.wantNested)
			}
			fields := make(map[string]*wireField, len(shape.Fields))
			for _, f := range shape.Fields {
				fields[f.Name] = f
			}
			for name, want := range tt.wantMirror {
				if got := fields[name].MirrorType(); got != want {
					t.Errorf("%s.MirrorType() = %q, want %q", name, got, want)
				}
			}
			for name, want := range tt.wantTS {
				if got := fields[name].TypescriptType("Root"); got != want {
					t.Errorf("%s.TypescriptType() = %q, want %q", name, got, want)
				}
			}
		})
	}
}

// Test_wireShape_emission pins the generated Go for a nested shape in both
// directions: the mirrors leaves first, one converter per non-flat struct, the
// pinned view spelling the source's fields, nil preserved on pointers and slices,
// and a whole that gofmt accepts.
func Test_wireShape_emission(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wirefixture"))
	report, err := walkFixture(t, structs, "Report")
	if err != nil {
		t.Fatalf("walk(Report) error = %v", err)
	}

	tests := []struct {
		name         string
		direction    wireDirection
		root         string
		wantContains []string
	}{
		{
			name: "to the mirror", direction: toMirror, root: "response",
			wantContains: []string{
				"mirrorSystem := func(src wirefixture.System) system {",
				"mirrorShip := func(src wirefixture.Ship) ship {",
				"mirrorResponse := func(src wirefixture.Report) *response {",
				"view := struct {\n\t\t\tName string\n\t\t\tReadings []*wirefixture.Reading\n\t\t\tLatest *wirefixture.Reading\n\t\t}(src)",
				"var readings []*reading\n\t\tif view.Readings != nil {",
				"if e == nil {\n\t\t\t\t\treadings = append(readings, nil)",
				"v := reading(*e)\n\t\t\t\treadings = append(readings, &v)",
				"var latest *reading\n\t\tif view.Latest != nil {\n\t\t\tv := reading(*view.Latest)\n\t\t\tlatest = &v\n\t\t}",
				"return system{Name: view.Name, Readings: readings, Latest: latest}",
				"systems = append(systems, mirrorSystem(e))",
				"return &response{ID: view.ID, Ship: mirrorShip(view.Ship), Primary: primary, Notes: view.Notes}",
			},
		},
		{
			name: "to the source", direction: toSource, root: "request",
			wantContains: []string{
				"sourceSystem := func(src system) wirefixture.System {",
				"sourceRequest := func(src request) *wirefixture.Report {",
				"return wirefixture.System(struct {\n\t\t\tName string\n\t\t\tReadings []*wirefixture.Reading\n\t\t\tLatest *wirefixture.Reading\n\t\t}{Name: src.Name, Readings: readings, Latest: latest})",
				"v := wirefixture.Reading(*e)",
				"return (*wirefixture.Report)(&struct {",
				"}{ID: src.ID, Ship: sourceShip(src.Ship), Primary: primary, Notes: src.Notes})",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decls := report.MirrorDecls()
			for _, want := range []string{
				"\ttype reading struct {\n\t\tValue float64 `json:\"value\"`\n\t\tAt time.Time `json:\"at\"`\n\t}\n",
				"\ttype system struct {\n\t\tName string `json:\"name\"`\n\t\tReadings []*reading `json:\"readings\"`\n\t\tLatest *reading `json:\"latest\"`\n\t}\n",
				"\ttype ship struct {\n\t\tName string `json:\"name\"`\n\t\tSystems []system `json:\"systems\"`\n\t}\n",
			} {
				if !strings.Contains(decls, want) {
					t.Errorf("MirrorDecls() missing %q:\n%s", want, decls)
				}
			}

			converters := report.Converters(tt.direction, tt.root)
			for _, want := range tt.wantContains {
				if !strings.Contains(converters, want) {
					t.Errorf("Converters() missing %q:\n%s", want, converters)
				}
			}
			if strings.Contains(converters, "Reading := func") {
				t.Errorf("a flat nested struct needs no converter; it converts whole:\n%s", converters)
			}

			// The root mirror is the template's; declare it here so the whole parses.
			src := "package p\n\nfunc f() {\n" + decls + "\ttype " + tt.root + " struct {\n"
			for _, f := range report.Fields {
				src += "\t\t" + f.Name + " " + f.MirrorType() + "\n"
			}
			src += "\t}\n\n" + converters + "\t_ = " + report.ConverterName(tt.direction, tt.root) + "\n}\n"
			if _, err := format.Source([]byte(src)); err != nil {
				t.Fatalf("format.Source() error = %v on:\n%s", err, src)
			}
		})
	}
}

func Test_typescriptNamespace(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wirefixture"))

	tests := []struct {
		name       string
		structName string
		want       string
	}{
		{name: "a flat shape declares nothing", structName: "Flat", want: ""},
		{
			name: "nested interfaces sit in the root's namespace, leaves first", structName: "Report",
			want: `export namespace InspectShip {
  export interface Reading {
    value: number;
    at: Date;
  }

  export interface System {
    name: string;
    readings: InspectShip.Reading[];
    latest: InspectShip.Reading;
  }

  export interface Ship {
    name: string;
    systems: InspectShip.System[];
  }
}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shape, err := walkFixture(t, structs, tt.structName)
			if err != nil {
				t.Fatalf("walk(%s) error = %v", tt.structName, err)
			}
			if got := typescriptNamespace("InspectShip", shape); got != tt.want {
				t.Errorf("typescriptNamespace() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func Test_checkOpaqueField(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wirefixture"))

	tests := []struct {
		name       string
		structName string
		field      string
		primaryKey bool
		wantErr    string
	}{
		{name: "a nested field beside flat ones passes", structName: "Board", field: "Recent"},
		{name: "a flat field is not opaque", structName: "Board", field: "Name"},
		{name: "a nested primary key is refused", structName: "Board", field: "Recent", primaryKey: true, wantErr: "Board.Recent: a nested field cannot be a primary key"},
		{name: "a filter tag on the nested field is refused", structName: "BoardFiltered", field: "Recent", wantErr: "BoardFiltered.Recent: a nested field is opaque and cannot carry the allow_filter tag"},
		{name: "a PII tag inside the nested struct is refused", structName: "BoardInnerTag", field: "Recent", wantErr: "BoardInnerTag.Recent: wirefixture.Tagged.Secret carries the conditions tag, which means nothing inside a nested field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shape, err := walkFixture(t, structs, tt.structName)
			if err != nil {
				t.Fatalf("walk(%s) error = %v", tt.structName, err)
			}
			var field *computedField
			for i, f := range structs[tt.structName].Fields() {
				if f.Name() == tt.field {
					field = &computedField{Field: f, wire: shape.Fields[i], IsPrimaryKey: tt.primaryKey}
				}
			}
			if field == nil {
				t.Fatalf("field %q not in %s", tt.field, tt.structName)
			}
			err = checkOpaqueField(tt.structName, field)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("checkOpaqueField() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("checkOpaqueField() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// fixtureRPCMethod builds an rpcMethodInfo over a wirefixture struct the way
// extraction does, for rendering the handler template.
func fixtureRPCMethod(t *testing.T, structs map[string]*parser.Struct, name string) *rpcMethodInfo {
	t.Helper()

	shape, err := walkFixture(t, structs, name)
	if err != nil {
		t.Fatalf("walk(%s) error = %v", name, err)
	}
	m := &rpcMethodInfo{Struct: structs[name], Form: rpcFormTxn, Request: shape}
	for i, f := range structs[name].Fields() {
		m.Fields = append(m.Fields, &rpcField{Field: f, wire: shape.Fields[i], namespace: name})
	}

	return m
}

// Test_rpcHandlerTemplate_request pins the request path: a flat request converts
// whole exactly as before, and a nested request declares its mirrors and builds
// the method's struct through the converters.
func Test_rpcHandlerTemplate_request(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wirefixture"))
	c := &client{}

	tests := []struct {
		name            string
		structName      string
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "flat request converts whole", structName: "Flat",
			wantContains:    []string{"\ttype request struct {\n\t\tID    ccc.UUID         `json:\"id\"`", "p := (*wirefixture.Flat)(params)"},
			wantNotContains: []string{"mirror", "sourceRequest", "view :="},
		},
		{
			name: "nested request declares mirrors and converts through them", structName: "Report",
			wantContains: []string{
				"\ttype reading struct {",
				"\ttype ship struct {",
				"\ttype request struct {\n\t\tID      ccc.UUID `json:\"id\"`\n\t\tShip    ship     `json:\"ship\"`\n\t\tPrimary *reading `json:\"primary\"`\n\t\tNotes   []string `json:\"notes\"`\n\t}",
				"sourceRequest := func(src request) *wirefixture.Report {",
				"p := sourceRequest(*params)",
			},
			wantNotContains: []string{"(*wirefixture.Report)(params)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out, err := c.generateTemplateOutput("rpcHandlerTemplate", rpcHandlerTemplate, &rpcHandlerData{
				Source:           "pkg/rpc",
				RPCMethod:        fixtureRPCMethod(t, structs, tt.structName),
				Package:          "app",
				ApplicationName:  "App",
				ReceiverName:     "a",
				ResourcesPackage: "resources",
			})
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			formatted, err := format.Source(out)
			if err != nil {
				t.Fatalf("format.Source() error = %v on:\n%s", err, out)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(formatted), want) {
					t.Errorf("handler missing %q:\n%s", want, formatted)
				}
			}
			for _, notWant := range tt.wantNotContains {
				if strings.Contains(string(formatted), notWant) {
					t.Errorf("handler must not contain %q:\n%s", notWant, formatted)
				}
			}
		})
	}
}

// fixtureAnsweringMethod builds an rpcMethodInfo over a wirefixture struct whose
// Execute answers, the way extraction does: one walker for request and result.
func fixtureAnsweringMethod(t *testing.T, structs map[string]*parser.Struct, name string) *rpcMethodInfo {
	t.Helper()

	signature, err := classifyExecute(structs[name])
	if err != nil {
		t.Fatalf("classifyExecute(%s) error = %v", name, err)
	}
	walker := newWireWalker(defaultTypescriptOverrides(), "wirefixture", "resources")
	request, err := walker.walk(structs[name])
	if err != nil {
		t.Fatalf("walk(%s) error = %v", name, err)
	}
	result, err := walker.walkNamed(signature.result, name+" result")
	if err != nil {
		t.Fatalf("walkNamed(%s result) error = %v", name, err)
	}
	m := &rpcMethodInfo{Struct: structs[name], Form: signature.form, Request: request, Result: result, ResultPointer: signature.resultPointer}
	for i, f := range structs[name].Fields() {
		m.Fields = append(m.Fields, &rpcField{Field: f, wire: request.Fields[i], namespace: name})
	}

	return m
}

// Test_rpcHandlerTemplate_answer pins the result path: the response mirror and the
// mirrors it reaches, the result captured inside the transaction and encoded after
// it, nil answering as an empty 200, and the method's TypeScript result type.
func Test_rpcHandlerTemplate_answer(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wirefixture"))
	c := &client{}
	m := fixtureAnsweringMethod(t, structs, "Inspect")

	out, err := c.generateTemplateOutput("rpcHandlerTemplate", rpcHandlerTemplate, &rpcHandlerData{
		Source:           "pkg/rpc",
		RPCMethod:        m,
		Package:          "app",
		ApplicationName:  "App",
		ReceiverName:     "a",
		ResourcesPackage: "resources",
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("format.Source() error = %v on:\n%s", err, out)
	}
	for _, want := range []string{
		"\ttype reading struct {",
		"\ttype ship struct {",
		"\ttype request struct {\n\t\tID ccc.UUID `json:\"id\"`\n\t}",
		"\ttype response struct {\n\t\tID      ccc.UUID `json:\"id\"`\n\t\tShip    ship     `json:\"ship\"`\n\t\tPrimary *reading `json:\"primary\"`\n\t\tNotes   []string `json:\"notes\"`\n\t}",
		"mirrorResponse := func(src wirefixture.Report) *response {",
		"var result *wirefixture.Report",
		"answer, err := p.Execute(ctx, txn, a.RPCClient())",
		"result = answer",
		"if result == nil {\n\t\t\treturn httpio.NewEncoder(w).Ok(nil)\n\t\t}",
		"return httpio.NewEncoder(w).Ok(mirrorResponse(*result))",
	} {
		if !strings.Contains(string(formatted), want) {
			t.Errorf("handler missing %q:\n%s", want, formatted)
		}
	}
	if strings.Count(string(formatted), "type reading struct") != 1 {
		t.Errorf("a struct the request and result share is mirrored once:\n%s", formatted)
	}

	ts, err := c.generateTemplateOutput("typescriptMethodsTemplate", typescriptMethodsTemplate, &tsMethodsData{
		File:       &typescriptGenerator{client: c},
		GenPrefix:  "zz_gen",
		RPCMethods: []*rpcMethodInfo{m},
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput(typescriptMethodsTemplate) error = %v", err)
	}
	for _, want := range []string{
		"export interface InspectResult {\n  id: string;\n  ship: Inspect.Ship;\n  primary: Inspect.Reading;\n  notes: string[];\n}",
		"export namespace Inspect {\n  export interface Reading {",
		"    answers: true,",
	} {
		if !strings.Contains(string(ts), want) {
			t.Errorf("methods TypeScript missing %q:\n%s", want, ts)
		}
	}
}

// Test_computedResourceHandlerTemplate_nested pins the computed path: a nested
// field is one opaque mirror field, and the row reaches its mirror through the
// converter instead of the whole-struct conversion a flat row keeps.
func Test_computedResourceHandlerTemplate_nested(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "wirefixture"))
	shape, err := walkFixture(t, structs, "Board")
	if err != nil {
		t.Fatalf("walk(Board) error = %v", err)
	}
	res := &computedResource{Struct: structs["Board"], Shape: shape}
	for i, f := range structs["Board"].Fields() {
		res.Fields = append(res.Fields, &computedField{Field: f, wire: shape.Fields[i], namespace: "Boards", IsPrimaryKey: f.Name() == "ID"})
	}

	c := &client{}
	out, err := c.generateTemplateOutput("computedResourceHandlerTemplate", computedResourceHandlerTemplate, &computedHandlerData{
		Source:          "pkg/computedresources",
		Resource:        res,
		Package:         "app",
		ComputedPackage: "wirefixture",
		ApplicationName: "App",
		ReceiverName:    "a",
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("format.Source() error = %v on:\n%s", err, out)
	}
	for _, want := range []string{
		"\ttype reading struct {\n\t\tValue float64   `json:\"value\"`\n\t\tAt    time.Time `json:\"at\"`\n\t}",
		"Recent []reading `json:\"recent\"",
		"mirrorBoard := func(src wirefixture.Board) *board {",
		"rec := mirrorBoard(*row)",
		"mirrorResponse := func(src wirefixture.Board) *response {",
		"rec := mirrorResponse(*row)",
		"rmap[\"recent\"] = rec.Recent",
	} {
		if !strings.Contains(string(formatted), want) {
			t.Errorf("handler missing %q:\n%s", want, formatted)
		}
	}
	for _, notWant := range []string{"(*board)(row)", "(*response)(row)"} {
		if strings.Contains(string(formatted), notWant) {
			t.Errorf("handler must not convert a nested row whole (%q):\n%s", notWant, formatted)
		}
	}
}
