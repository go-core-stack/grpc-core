package gensdk

import (
	"bytes"
	"strings"
	"text/template"

	"google.golang.org/grpc/grpclog"

	"github.com/go-core-stack/grpc-core/internal/casing"
	"github.com/go-core-stack/grpc-core/internal/descriptor"
)

type param struct {
	*descriptor.File
	Imports            []descriptor.GoPackage
	UseRequestContext  bool
	RegisterFuncSuffix string
	AllowPatchFeature  bool
	OmitPackageDoc     bool
	PathPrefix         string
}

type trailerParams struct {
	P                  param
	Services           []*descriptor.Service
	UseRequestContext  bool
	RegisterFuncSuffix string
	PathPrefix         string
}

// getMethodComment retrieves leading comments for a given service/method
func getMethodComment(p param, serviceIndex, methodIndex int) []string {
	file := p.File
	if file.SourceCodeInfo == nil {
		return nil
	}

	// The path that identifies the method node in the AST of the file
	// According to descriptor.proto, method path = [6, service_index, 2, method_index]
	// where:
	//   6 => service
	//   2 => method (within service)
	path := []int32{6, int32(serviceIndex), 2, int32(methodIndex)}

	for _, loc := range file.GetSourceCodeInfo().GetLocation() {
		if equalPath(loc.GetPath(), path) {
			str := loc.GetLeadingComments()
			lines := strings.Split(strings.TrimSpace(str), "\n")
			for i, s := range lines {
				lines[i] = strings.TrimSpace(s)
			}
			return lines
		}
	}
	return nil
}

// helper to compare paths
func equalPath(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func getImports(services []*descriptor.Service) []string {
	imports := []string{"context", "fmt", "io", "net/http"}
	importMap := map[string]bool{}
	for _, s := range services {
		for _, m := range s.Methods {
			if len(m.Bindings) == 0 {
				continue
			}
			b := m.Bindings[0]
			if len(b.PathParams) != 0 {
				importMap["strings"] = true
				importMap["net/url"] = true
			}
			if hasQueryParams(m) {
				importMap["net/url"] = true
			}
			if b.Body != nil {
				importMap["bytes"] = true
			}
		}
	}

	_, ok := importMap["strings"]
	if ok {
		imports = append(imports, "strings")
	}

	_, ok = importMap["net/url"]
	if ok {
		imports = append(imports, "net/url")
	}

	_, ok = importMap["bytes"]
	if ok {
		imports = append(imports, "bytes")
	}

	return imports
}

func getCamelCasing(val string) string {
	return casing.Camel(val)
}

func hasQueryParams(m *descriptor.Method) bool {
	if len(m.Bindings) == 0 {
		return false
	}

	b := m.Bindings[0]
	// if body is expected with *, then skip going through
	// query params
	if b.Body != nil && len(b.Body.FieldPath) == 0 {
		return false
	}

	// capture all available fields in the request map
	fields := map[string]bool{}
	for _, f := range b.Method.RequestType.Fields {
		fields[f.GetName()] = true
	}

	// skip the field that are supposed to be sent as
	// part of Body in http request non wildcard value
	if b.Body != nil {
		delete(fields, b.Body.FieldPath.String())
	}

	// skip the fields that are supposed to be sent as
	// path params
	for _, p := range b.PathParams {
		delete(fields, p.FieldPath.String())
	}

	// include remaining fields in the query params list
	for _, f := range b.Method.RequestType.Fields {
		// iterate through the list instead of map
		// to maintain the order in code generation
		// ensuring the code doesn't keep changing
		// on every iteration of generation
		val := f.GetName()
		_, ok := fields[val]
		if ok {
			return true
		}
	}

	return false
}

func getQueryParams(m descriptor.Method) []string {
	list := []string{}
	if len(m.Bindings) == 0 {
		return list
	}

	b := m.Bindings[0]
	// if body is expected with *, then skip going through
	// query params
	if b.Body != nil && len(b.Body.FieldPath) == 0 {
		return list
	}

	// capture all available fields in the request map
	fields := map[string]bool{}
	for _, f := range b.Method.RequestType.Fields {
		fields[f.GetName()] = true
	}

	// skip the field that are supposed to be sent as
	// part of Body in http request non wildcard value
	if b.Body != nil {
		delete(fields, b.Body.FieldPath.String())
	}

	// skip the fields that are supposed to be sent as
	// path params
	for _, p := range b.PathParams {
		delete(fields, p.FieldPath.String())
	}

	// include remaining fields in the query params list
	for _, f := range b.Method.RequestType.Fields {
		// iterate through the list instead of map
		// to maintain the order in code generation
		// ensuring the code doesn't keep changing
		// on every iteration of generation
		val := f.GetName()
		_, ok := fields[val]
		if ok {
			list = append(list, val)
		}
	}

	return list
}

func applyTemplate(p param, reg *descriptor.Registry) (string, error) {
	var targetServices []*descriptor.Service

	for _, msg := range p.Messages {
		msgName := casing.Camel(*msg.Name)
		msg.Name = &msgName
	}

	for _, svc := range p.Services {
		var methodWithBindingsSeen bool
		svcName := casing.Camel(*svc.Name)
		svc.Name = &svcName

		for _, meth := range svc.Methods {
			if grpclog.V(2) {
				grpclog.Infof("Processing %s.%s", svc.GetName(), meth.GetName())
			}
			methName := casing.Camel(*meth.Name)
			meth.Name = &methName
			for _, b := range meth.Bindings {
				if err := reg.CheckDuplicateAnnotation(b.HTTPMethod, b.PathTmpl.Template, svc); err != nil {
					return "", err
				}

				methodWithBindingsSeen = true
			}
		}
		if methodWithBindingsSeen {
			targetServices = append(targetServices, svc)
		}
	}
	if len(targetServices) == 0 {
		return "", errNoTargetService
	}

	tp := trailerParams{
		P:                  p,
		Services:           targetServices,
		UseRequestContext:  p.UseRequestContext,
		RegisterFuncSuffix: p.RegisterFuncSuffix,
		PathPrefix:         p.PathPrefix,
	}

	w := bytes.NewBuffer(nil)
	if err := rtemplate.Execute(w, tp); err != nil {
		return "", err
	}

	return w.String(), nil
}

var (
	rtemplate = template.Must(template.New("header").Funcs(
		template.FuncMap{
			"GetCamelCasing":   getCamelCasing,
			"GetQueryParams":   getQueryParams,
			"GetImports":       getImports,
			"GetMethodComment": getMethodComment,
		},
	).Parse(`
// Code generated by protoc-gen-sdk. DO NOT EDIT.
// source: {{.P.GetName}}

package {{.P.GoPkg.Name}}

{{- $param := .P }}
{{- $imp := GetImports .Services }}
{{- if $imp }}
import (
	{{- range $i := $imp }}
	"{{ $i }}"
	{{- end }}

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	auth "github.com/go-core-stack/auth/client"
)
{{- end }}

{{range $sid, $svc := .Services}}
// {{$svc.GetName}}Service
// provides SDK wrapper methods for {{$svc.GetName}} service
type {{$svc.GetName}}Service interface {
	{{- range $mid, $m := $svc.Methods }}
	{{- range $comment := GetMethodComment $param $sid $mid }}
	// {{ $comment }}
	{{- end }}
	{{$m.GetName}}(ctx context.Context, req *{{$m.RequestType.GetName}}) (*{{$m.ResponseType.GetName}}, error)

	{{- end }}
}

type impl{{$svc.GetName}}Service struct {
	client auth.Client
}

// New{{$svc.GetName}}Service
// creates a new SDK wrapper for {{$svc.GetName}} service
// function expects to be provided with an auth client to
// trigger request to service
func New{{$svc.GetName}}Service(client auth.Client) {{$svc.GetName}}Service {
	return &impl{{$svc.GetName}}Service{
		client: client,
	}
}

{{range $m := $svc.Methods}}
func (s *impl{{$svc.GetName}}Service) {{$m.GetName}}(ctx context.Context, req *{{$m.RequestType.GetName}}) (*{{$m.ResponseType.GetName}}, error) {
	{{- $b := (index $m.Bindings 0) }}
	uri := "{{ $b.PathTmpl.Template }}"

	{{- if gt (len $b.PathParams) 0 }}
	// ensure replacing the variables in the uri before triggering client
	{{- end }}
	{{- range $p := $b.PathParams }}
	uri = strings.Replace(uri, "{"+"{{ $p.Target.Name }}"+"}", url.PathEscape(fmt.Sprintf("%v", req.{{GetCamelCasing $p.Target.Name }})), -1)
	{{- end }}

	// use marshaller for grpc Gateway since we are working protobuf files
	marshaller := &runtime.JSONPb{}
	{{ if $b.Body }}
	inData, _ := marshaller.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, {{ $b.HTTPMethod | printf "%q" }}, uri, bytes.NewBuffer(inData))
	{{- else }}
	r, err := http.NewRequestWithContext(ctx, {{ $b.HTTPMethod | printf "%q" }}, uri, nil)
	{{- end }}
	if err != nil {
		return nil, fmt.Errorf("failed create request: %s", err) 
	}

	{{- $qList := GetQueryParams $m }}
	{{- if $qList }}
	q := url.Values{}
	{{- range $q := $qList }}
	q.Add("{{ $q }}", fmt.Sprintf("%v", req.{{GetCamelCasing $q }}))
	{{- end }}
	r.URL.RawQuery = q.Encode()
	{{- end }}

	r.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(r)
	if err != nil {
		return nil, err
	}

	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	outBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	out := &{{ $m.ResponseType.GetName }}{}
	err = marshaller.Unmarshal(outBytes, out)
	if err != nil {
		return nil, err
	}

	return out, nil
}
{{end}}

{{end}}`))
)
