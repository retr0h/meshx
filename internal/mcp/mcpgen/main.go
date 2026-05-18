// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

// Command mcpgen reads the meshx OpenAPI 3.0 spec and emits
// tools_gen.go — a generated Go file containing all MCP tool
// registrations, args structs, and handler methods. Each non-SSE
// operation becomes one tool an LLM agent can call through the
// MCP server.
//
// Registered as a `tool` in go.mod (same pattern as emojigen /
// dumpspec / oapi-codegen) so jennifer/jen and kin-openapi land in
// the require graph through a real-build subpackage. Invoked via
// `go tool` from the parent mcp package's go:generate directive.
package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/getkin/kin-openapi/openapi3"
)

const (
	mcpsdkPkg = "github.com/modelcontextprotocol/go-sdk/mcp"
	genPkg    = "github.com/retr0h/meshx/internal/sdk/gen"
)

func main() {
	spec := flag.String("spec", "internal/sdk/gen/api.yaml", "path to OpenAPI 3.0 spec")
	out := flag.String("out", "internal/mcp/tools_gen.go", "output Go file")
	flag.Parse()

	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(*spec)
	if err != nil {
		log.Fatalf("mcpgen: load spec %s: %v", *spec, err)
	}
	// Skip strict validation — the spec uses `const` (an OAS 3.1
	// keyword) inside SSE response schemas that we skip anyway.
	// Loading alone is sufficient for our purposes.

	ops := collectOps(doc)
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].toolName < ops[j].toolName
	})

	if err := emit(*out, ops); err != nil {
		log.Fatalf("mcpgen: emit %s: %v", *out, err)
	}
	log.Printf("mcpgen: wrote %s — %d tools", *out, len(ops))
}

// op holds everything we need to emit one MCP tool.
type op struct {
	operationID  string // e.g. "send-message"
	toolName     string // e.g. "send_message"
	pascalName   string // e.g. "SendMessage"
	description  string // operation summary + description
	method       string // HTTP method
	pathParams   []param
	queryParams  []param
	headerParams []param
	bodyFields   []bodyField
	bodyRef      string // e.g. "SendMessageRequest" — the schema name
	// Response handling.
	jsonStatus int  // 200 or 202; 0 means no JSON body (204 etc)
	noContent  bool // true for 204 responses
}

// param is a path / query / header parameter.
type param struct {
	name        string // as in the spec, e.g. "radio_id"
	goName      string // PascalCase Go field name
	description string
	in          string // "path" | "query" | "header"
	typ         string // "string" | "int64"
	format      string // OpenAPI format, e.g. "int64"
	required    bool
	enum        []string
}

// bodyField is one property from the request body schema.
type bodyField struct {
	jsonName    string // e.g. "to_num"
	goName      string // e.g. "ToNum"
	description string
	typ         string // Go type for the args struct field
	genTyp      string // Go type on the gen body struct
	required    bool
	isPointer   bool // true when the gen struct uses *T
}

// collectOps walks every path+method in the spec and builds op
// descriptors, skipping SSE streaming endpoints.
func collectOps(doc *openapi3.T) []op {
	var ops []op

	// Sorted paths for deterministic output.
	paths := make([]string, 0, len(doc.Paths.Map()))
	for p := range doc.Paths.Map() {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		item := doc.Paths.Map()[path]
		for _, pair := range methodOps(item) {
			oper := pair.op
			if oper == nil || oper.OperationID == "" {
				continue
			}
			// Skip SSE streaming endpoints.
			if isSSE(oper) {
				continue
			}

			o := op{
				operationID: oper.OperationID,
				toolName:    toSnake(oper.OperationID),
				pascalName:  toPascal(oper.OperationID),
				description: oper.Description,
				method:      pair.method,
			}

			// Collect parameters.
			for _, pRef := range oper.Parameters {
				p := pRef.Value
				if p == nil {
					continue
				}
				typ, format := schemaGoType(p.Schema.Value)
				pm := param{
					name:        p.Name,
					goName:      toPascal(p.Name),
					description: p.Description,
					in:          p.In,
					typ:         typ,
					format:      format,
					required:    p.Required,
				}
				if p.Schema.Value != nil && len(p.Schema.Value.Enum) > 0 {
					for _, e := range p.Schema.Value.Enum {
						if s, ok := e.(string); ok {
							pm.enum = append(pm.enum, s)
						}
					}
				}
				switch p.In {
				case "path":
					o.pathParams = append(o.pathParams, pm)
				case "query":
					o.queryParams = append(o.queryParams, pm)
				case "header":
					o.headerParams = append(o.headerParams, pm)
				}
			}

			// Collect request body fields.
			if oper.RequestBody != nil && oper.RequestBody.Value != nil {
				if ct := oper.RequestBody.Value.Content.Get("application/json"); ct != nil {
					if ct.Schema != nil && ct.Schema.Ref != "" {
						schemaName := refName(ct.Schema.Ref)
						o.bodyRef = schemaName
						if schema := ct.Schema.Value; schema != nil {
							reqSet := make(map[string]bool)
							for _, r := range schema.Required {
								reqSet[r] = true
							}
							// Sorted properties for deterministic output.
							propNames := make([]string, 0, len(schema.Properties))
							for name := range schema.Properties {
								propNames = append(propNames, name)
							}
							sort.Strings(propNames)
							for _, name := range propNames {
								propRef := schema.Properties[name]
								prop := propRef.Value
								if prop == nil {
									continue
								}
								// Skip the $schema field — it's a Huma
								// housekeeping field, not a user input.
								if name == "$schema" {
									continue
								}
								// Skip readOnly fields.
								if prop.ReadOnly {
									continue
								}
								goTyp, _ := schemaGoType(prop)
								bf := bodyField{
									jsonName:    name,
									goName:      toPascal(name),
									description: prop.Description,
									typ:         goTyp,
									genTyp:      goTyp,
									required:    reqSet[name],
								}
								// Determine if the gen struct uses a
								// pointer for this field. Required fields
								// with no default are value types;
								// optional fields are pointers.
								if !reqSet[name] {
									bf.isPointer = true
								}
								o.bodyFields = append(o.bodyFields, bf)
							}
						}
					}
				}
			}

			// Determine the JSON response status code.
			o.jsonStatus, o.noContent = responseInfo(oper)

			ops = append(ops, o)
		}
	}
	return ops
}

// methodPair pairs an HTTP method string with its Operation.
type methodPair struct {
	method string
	op     *openapi3.Operation
}

// methodOps returns method+operation pairs in a stable order.
func methodOps(item *openapi3.PathItem) []methodPair {
	var pairs []methodPair
	if item.Get != nil {
		pairs = append(pairs, methodPair{"GET", item.Get})
	}
	if item.Post != nil {
		pairs = append(pairs, methodPair{"POST", item.Post})
	}
	if item.Put != nil {
		pairs = append(pairs, methodPair{"PUT", item.Put})
	}
	if item.Patch != nil {
		pairs = append(pairs, methodPair{"PATCH", item.Patch})
	}
	if item.Delete != nil {
		pairs = append(pairs, methodPair{"DELETE", item.Delete})
	}
	return pairs
}

// isSSE returns true when the operation's success response uses
// text/event-stream content type.
func isSSE(op *openapi3.Operation) bool {
	if op.Responses == nil {
		return false
	}
	for _, respRef := range op.Responses.Map() {
		if respRef.Value == nil {
			continue
		}
		if respRef.Value.Content.Get("text/event-stream") != nil {
			return true
		}
	}
	return false
}

// responseInfo determines the JSON status code for the success
// response. Returns (statusCode, noContent).
func responseInfo(op *openapi3.Operation) (int, bool) {
	if op.Responses == nil {
		return 0, true
	}
	// Check status codes in priority order.
	for _, code := range []string{"200", "202", "204"} {
		respRef := op.Responses.Status(codeInt(code))
		if respRef == nil {
			continue
		}
		if code == "204" {
			return 0, true
		}
		if respRef.Value != nil && respRef.Value.Content.Get("application/json") != nil {
			return codeInt(code), false
		}
		// 202 with no body (like delete-channel's 202).
		if respRef.Value != nil && respRef.Value.Content.Get("application/json") == nil {
			return 0, true
		}
	}
	return 0, true
}

func codeInt(s string) int {
	switch s {
	case "200":
		return 200
	case "202":
		return 202
	case "204":
		return 204
	}
	return 0
}

// emit writes the generated Go file using dave/jennifer.
func emit(
	path string,
	ops []op,
) error {
	f := jen.NewFile("mcp")
	f.ImportAlias(mcpsdkPkg, "mcpsdk")
	f.ImportAlias(genPkg, "gen")
	f.HeaderComment("Code generated by mcpgen; DO NOT EDIT.")
	f.Line()

	// Emit registerGeneratedTools.
	f.Comment("registerGeneratedTools wires every spec-derived MCP tool onto s.mcp.")
	f.Comment("Called from registerTools() in tools.go.")
	f.Func().Params(jen.Id("s").Op("*").Id("Server")).Id("registerGeneratedTools").Params().Block(
		emitRegistrations(ops)...,
	)

	// Emit args structs and handler methods.
	for _, o := range ops {
		f.Line()
		emitArgsStruct(f, o)
		f.Line()
		emitHandler(f, o)
	}

	return f.Save(path)
}

// emitRegistrations returns the jen statements for all AddTool calls.
func emitRegistrations(ops []op) []jen.Code {
	stmts := make([]jen.Code, 0, len(ops))
	for _, o := range ops {
		stmts = append(
			stmts,
			jen.Qual(mcpsdkPkg, "AddTool").Call(
				jen.Id("s").Dot("mcp"),
				jen.Op("&").Qual(mcpsdkPkg, "Tool").Values(jen.Dict{
					jen.Id("Name"):        jen.Lit(o.toolName),
					jen.Id("Description"): jen.Lit(o.description),
				}),
				jen.Id("s").Dot("tool"+o.pascalName),
			),
		)
	}
	return stmts
}

// emitArgsStruct writes the args struct for one tool.
func emitArgsStruct(
	f *jen.File,
	o op,
) {
	fields := argsFields(o)
	if len(fields) == 0 {
		// No args — the handler will use struct{}.
		return
	}
	structName := lcFirst(o.pascalName) + "Args"
	f.Type().Id(structName).Struct(fields...)
}

// argsFields builds the jen field list for the args struct.
func argsFields(o op) []jen.Code {
	fields := make(
		[]jen.Code,
		0,
		len(o.pathParams)+len(o.queryParams)+len(o.headerParams)+len(o.bodyFields),
	)

	// Path params.
	for _, p := range o.pathParams {
		fields = append(
			fields,
			argsField(p.goName, p.jsonName(o), p.description, goType(p.typ), !p.required, false),
		)
	}
	// Query params.
	for _, p := range o.queryParams {
		fields = append(
			fields,
			argsField(p.goName, p.name, p.description, goType(p.typ), !p.required, false),
		)
	}
	// Header params.
	for _, p := range o.headerParams {
		fields = append(
			fields,
			argsField(
				p.goName,
				headerJSONName(p.name),
				p.description,
				goType(p.typ),
				!p.required,
				false,
			),
		)
	}
	// Body fields.
	for _, bf := range o.bodyFields {
		optBool := !bf.required && bf.typ == "bool"
		fields = append(
			fields,
			argsField(
				bf.goName,
				bf.jsonName,
				bf.description,
				goType(bf.typ),
				!bf.required,
				optBool,
			),
		)
	}

	return fields
}

// jsonName returns the JSON tag name for a parameter. For path
// params it's the snake_case name from the spec.
func (p param) jsonName(_ op) string {
	return p.name
}

// headerJSONName converts a header name like "Idempotency-Key" to
// a lowercase JSON-friendly tag "idempotency_key".
func headerJSONName(name string) string {
	return strings.ToLower(toSnake(name))
}

// argsField builds a single struct field with json + jsonschema tags.
// Optional bool fields use *bool so nil means "not supplied" and
// false is a valid value (matches the hand-written pattern).
func argsField(
	goName string,
	jsonName string,
	description string,
	goTyp jen.Code,
	optional bool,
	optionalBool bool,
) jen.Code {
	jsonTag := jsonName
	if optional {
		jsonTag += ",omitempty"
	}
	fieldType := goTyp
	if optionalBool {
		fieldType = jen.Op("*").Bool()
	}
	return jen.Id(goName).Add(fieldType).Tag(map[string]string{
		"json":       jsonTag,
		"jsonschema": description,
	})
}

// goType maps a type string to a jen.Code.
func goType(typ string) jen.Code {
	switch typ {
	case "int64":
		return jen.Int64()
	case "int32":
		return jen.Int32()
	case "int":
		return jen.Int()
	case "float64":
		return jen.Float64()
	case "bool":
		return jen.Bool()
	default:
		return jen.String()
	}
}

// emitHandler writes the tool handler method.
func emitHandler(
	f *jen.File,
	o op,
) {
	hasArgs := len(o.pathParams)+len(o.queryParams)+len(o.headerParams)+len(o.bodyFields) > 0
	argsType := jen.Struct()
	if hasArgs {
		argsType = jen.Id(lcFirst(o.pascalName) + "Args")
	}

	f.Func().Params(jen.Id("s").Op("*").Id("Server")).Id("tool"+o.pascalName).Params(
		jen.Id("ctx").Qual("context", "Context"),
		jen.Id("_").Op("*").Qual(mcpsdkPkg, "CallToolRequest"),
		jen.Id("args").Add(argsType),
	).Params(
		jen.Op("*").Qual(mcpsdkPkg, "CallToolResult"),
		jen.Any(),
		jen.Error(),
	).Block(
		handlerBody(o)...,
	)
}

// handlerBody generates the statements inside a handler method.
func handlerBody(o op) []jen.Code {
	stmts := make([]jen.Code, 0, 8)

	// Build the SDK client call arguments.
	callArgs := []jen.Code{jen.Id("ctx")}

	// Path params are positional args after ctx.
	for _, p := range o.pathParams {
		expr := jen.Id("args").Dot(p.goName)
		// oapi-codegen uses int64 for integer path params.
		if p.typ == "int64" || p.typ == "int32" {
			expr = jen.Int64().Call(jen.Id("args").Dot(p.goName))
		}
		callArgs = append(callArgs, expr)
	}

	// If there are query or header params, build a params struct.
	hasParams := len(o.queryParams)+len(o.headerParams) > 0
	if hasParams {
		stmts = append(stmts, buildParamsStruct(o)...)
		callArgs = append(callArgs, jen.Id("params"))
	}

	// If there's a body, build the body struct.
	hasBody := len(o.bodyFields) > 0
	if hasBody {
		stmts = append(stmts, buildBodyStruct(o)...)
		callArgs = append(callArgs, jen.Id("body"))
	}

	// Make the SDK call.
	methodName := o.pascalName + "WithResponse"
	stmts = append(
		stmts,
		jen.List(jen.Id("resp"), jen.Err()).
			Op(":=").
			Id("s").
			Dot("client").
			Dot(methodName).
			Call(callArgs...),
		jen.If(jen.Err().Op("!=").Nil()).Block(
			jen.Return(jen.Nil(), jen.Nil(), jen.Qual("fmt", "Errorf").Call(
				jen.Lit(o.toolName+": %w"),
				jen.Err(),
			)),
		),
	)

	// Handle response.
	stmts = append(stmts, responseHandling(o)...)

	return stmts
}

// buildParamsStruct emits the gen.*Params construction.
func buildParamsStruct(o op) []jen.Code {
	stmts := make([]jen.Code, 0, 8)
	paramsType := o.pascalName + "Params"

	stmts = append(
		stmts,
		jen.Var().Id("params").Op("*").Qual(genPkg, paramsType),
	)

	// For each query/header param, conditionally set it on the
	// params struct. We lazily init params.
	for _, p := range o.queryParams {
		stmts = append(stmts, paramAssign(paramsType, p)...)
	}
	for _, p := range o.headerParams {
		stmts = append(stmts, paramAssign(paramsType, p)...)
	}

	return stmts
}

// paramAssign emits the if-block that initializes the params
// struct and sets one field.
func paramAssign(
	paramsType string,
	p param,
) []jen.Code {
	zeroCheck := zeroCheckExpr(p.typ, jen.Id("args").Dot(p.goName))

	var assign jen.Code
	if p.enum != nil {
		// Enum params need a type cast.
		enumType := paramsType + p.goName
		assign = jen.Id("v").Op(":=").Qual(genPkg, enumType).Call(jen.Id("args").Dot(p.goName))
	} else if p.typ == "int64" || p.typ == "int32" {
		assign = jen.Id("v").Op(":=").Int64().Call(jen.Id("args").Dot(p.goName))
	} else {
		assign = jen.Id("v").Op(":=").Id("args").Dot(p.goName)
	}

	return []jen.Code{
		jen.If(zeroCheck).Block(
			jen.If(jen.Id("params").Op("==").Nil()).Block(
				jen.Id("params").Op("=").Op("&").Qual(genPkg, paramsType).Values(),
			),
			assign,
			jen.Id("params").Dot(p.goName).Op("=").Op("&").Id("v"),
		),
	}
}

// buildBodyStruct emits the gen.*JSONRequestBody construction.
func buildBodyStruct(o op) []jen.Code {
	stmts := make([]jen.Code, 0, 8)
	bodyType := o.pascalName + "JSONRequestBody"

	// Start with an empty body.
	stmts = append(
		stmts,
		jen.Id("body").Op(":=").Qual(genPkg, bodyType).Values(),
	)

	// Set each field. Required fields are set directly; optional
	// fields are set conditionally with pointer indirection.
	for _, bf := range o.bodyFields {
		if bf.required && !bf.isPointer {
			// Direct assignment — required fields.
			stmts = append(stmts, directBodyAssign(bf))
		} else {
			// Conditional assignment — optional fields.
			stmts = append(stmts, optionalBodyAssign(bf)...)
		}
	}

	return stmts
}

// directBodyAssign sets a required body field directly.
func directBodyAssign(bf bodyField) jen.Code {
	expr := jen.Id("args").Dot(bf.goName)
	// Handle type conversions between args and gen types.
	if bf.typ != bf.genTyp {
		expr = jen.Id(bf.genTyp).Call(expr)
	}
	return jen.Id("body").Dot(bf.goName).Op("=").Add(expr)
}

// optionalBodyAssign emits an if-block that conditionally sets an
// optional body field.
func optionalBodyAssign(bf bodyField) []jen.Code {
	isOptBool := !bf.required && bf.typ == "bool"

	if isOptBool {
		// Optional bools use *bool in the args struct; nil = not
		// supplied, false is a valid value. Forward the pointer
		// directly.
		return []jen.Code{
			jen.If(jen.Id("args").Dot(bf.goName).Op("!=").Nil()).Block(
				jen.Id("body").Dot(bf.goName).Op("=").Id("args").Dot(bf.goName),
			),
		}
	}

	zeroCheck := zeroCheckExpr(bf.typ, jen.Id("args").Dot(bf.goName))

	var innerStmts []jen.Code
	if bf.typ == "int64" || bf.typ == "int32" {
		innerStmts = append(
			innerStmts,
			jen.Id("v").Op(":=").Id(bf.genTyp).Call(jen.Id("args").Dot(bf.goName)),
			jen.Id("body").Dot(bf.goName).Op("=").Op("&").Id("v"),
		)
	} else {
		innerStmts = append(
			innerStmts,
			jen.Id("v").Op(":=").Id("args").Dot(bf.goName),
			jen.Id("body").Dot(bf.goName).Op("=").Op("&").Id("v"),
		)
	}

	return []jen.Code{
		jen.If(zeroCheck).Block(innerStmts...),
	}
}

// responseHandling emits the response check + return.
func responseHandling(o op) []jen.Code {
	if o.noContent {
		// 204 or bodyless 202 — check status range, return a text
		// confirmation.
		return []jen.Code{
			jen.If(
				jen.Id("resp").Dot("StatusCode").Call().Op("<").Lit(200).Op("||").
					Id("resp").Dot("StatusCode").Call().Op(">=").Lit(300),
			).Block(
				jen.Return(jen.Nil(), jen.Nil(), jen.Qual("fmt", "Errorf").Call(
					jen.Lit(o.toolName+": daemon returned %s"),
					jen.Id("resp").Dot("Status").Call(),
				)),
			),
			jen.Return(
				jen.Id("textResult").Call(jen.Qual("fmt", "Sprintf").Call(
					jen.Lit(o.toolName+": ok (%s)"),
					jen.Id("resp").Dot("Status").Call(),
				)),
				jen.Nil(),
				jen.Nil(),
			),
		}
	}

	jsonField := fmt.Sprintf("JSON%d", o.jsonStatus)
	return []jen.Code{
		jen.If(jen.Id("resp").Dot(jsonField).Op("==").Nil()).Block(
			jen.Return(jen.Nil(), jen.Nil(), jen.Qual("fmt", "Errorf").Call(
				jen.Lit(o.toolName+": daemon returned %s"),
				jen.Id("resp").Dot("Status").Call(),
			)),
		),
		jen.Return(
			jen.Id("textResult").Call(jen.Id("jsonOrErr").Call(jen.Id("resp").Dot(jsonField))),
			jen.Nil(),
			jen.Nil(),
		),
	}
}

// zeroCheckExpr returns a jen expression that checks whether the
// given value is non-zero for its type.
func zeroCheckExpr(
	typ string,
	expr *jen.Statement,
) *jen.Statement {
	switch typ {
	case "int64", "int32", "int", "float64":
		return expr.Clone().Op("!=").Lit(0)
	case "bool":
		return expr.Clone()
	default:
		return expr.Clone().Op("!=").Lit("")
	}
}

// --- Name conversion helpers ---

// toSnake converts "send-message" → "send_message".
func toSnake(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// toPascal converts "send-message" → "SendMessage",
// "radio_id" → "RadioId", "Last-Event-ID" → "LastEventId".
func toPascal(s string) string {
	// Split on hyphens and underscores.
	segments := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_'
	})
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		parts = append(parts, ucFirst(segment))
	}
	return strings.Join(parts, "")
}

// ucFirst uppercases the first byte of s.
func ucFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// lcFirst lowercases the first byte of s.
func lcFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// refName extracts "Foo" from "#/components/schemas/Foo".
func refName(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

// schemaGoType returns the Go type string for an OpenAPI schema.
func schemaGoType(schema *openapi3.Schema) (string, string) {
	if schema == nil {
		return "string", ""
	}
	switch schema.Type.Slice()[0] {
	case "integer":
		switch schema.Format {
		case "int32":
			return "int32", "int32"
		default:
			return "int64", schema.Format
		}
	case "number":
		return "float64", schema.Format
	case "boolean":
		return "bool", ""
	default:
		return "string", ""
	}
}
