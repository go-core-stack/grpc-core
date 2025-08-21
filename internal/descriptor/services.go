package descriptor

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	options "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	myoptions "github.com/go-core-stack/grpc-core/coreapis/api"
	"github.com/go-core-stack/grpc-core/internal/httprule"
)

// Regular expression to validate kebab-case format
var kebabCaseRegex = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// validateKebabCase checks if a string is in valid kebab-case format
func validateKebabCase(field, value string) error {
	if value == "" {
		return nil // Empty values are allowed
	}
	if !kebabCaseRegex.MatchString(value) {
		return fmt.Errorf("field '%s' with value '%s' is not in kebab-case format", field, value)
	}
	return nil
}

// loadServices registers services and their methods from "targetFile" to "r".
// It must be called after loadFile is called for all files so that loadServices
// can resolve names of message types and their fields.
func (r *Registry) loadServices(file *File) error {
	if grpclog.V(1) {
		grpclog.Infof("Loading services from %s", file.GetName())
	}
	var svcs []*Service
	for _, sd := range file.GetService() {
		if grpclog.V(2) {
			grpclog.Infof("Registering %s", sd.GetName())
		}
		svc := &Service{
			File:                   file,
			ServiceDescriptorProto: sd,
			ForcePrefixedName:      r.standalone,
		}
		for _, md := range sd.GetMethod() {
			if grpclog.V(2) {
				grpclog.Infof("Processing %s.%s", sd.GetName(), md.GetName())
			}
			opts, err := extractAPIOptions(md)
			if err != nil {
				grpclog.Errorf("Failed to extract HttpRule from %s.%s: %v", svc.GetName(), md.GetName(), err)
				return err
			}
			role, err := extractRoleOptions(md)
			if err != nil {
				grpclog.Errorf("Failed to extract HttpRule from %s.%s: %v", svc.GetName(), md.GetName(), err)
				return err
			}
			optsList := r.LookupExternalHTTPRules((&Method{Service: svc, MethodDescriptorProto: md}).FQMN())
			if opts != nil {
				optsList = append(optsList, opts)
			}
			if len(optsList) == 0 {
				if r.generateUnboundMethods {
					defaultOpts, err := defaultAPIOptions(svc, md)
					if err != nil {
						grpclog.Errorf("Failed to generate default HttpRule from %s.%s: %v", svc.GetName(), md.GetName(), err)
						return err
					}
					optsList = append(optsList, defaultOpts)
				} else {
					if grpclog.V(1) {
						logFn := grpclog.Infof
						if r.warnOnUnboundMethods {
							logFn = grpclog.Warningf
						}
						logFn("No HttpRule found for method: %s.%s", svc.GetName(), md.GetName())
					}
				}
			}
			meth, err := r.newMethod(svc, md, optsList, role)
			if err != nil {
				return err
			}
			svc.Methods = append(svc.Methods, meth)
			r.meths[meth.FQMN()] = meth
		}
		if len(svc.Methods) == 0 {
			continue
		}
		if grpclog.V(2) {
			grpclog.Infof("Registered %s with %d method(s)", svc.GetName(), len(svc.Methods))
		}
		svcs = append(svcs, svc)
	}
	file.Services = svcs
	return nil
}

func (r *Registry) newMethod(svc *Service, md *descriptorpb.MethodDescriptorProto, optsList []*options.HttpRule, role *myoptions.Role) (*Method, error) {
	requestType, err := r.LookupMsg(svc.File.GetPackage(), md.GetInputType())
	if err != nil {
		return nil, err
	}
	responseType, err := r.LookupMsg(svc.File.GetPackage(), md.GetOutputType())
	if err != nil {
		return nil, err
	}
	meth := &Method{
		Service:               svc,
		MethodDescriptorProto: md,
		RequestType:           requestType,
		ResponseType:          responseType,
	}

	if role != nil {
		meth.Role = &Role{
			Resource: role.Resource,
			Scopes:   role.Scope,
			Verb:     role.Verb,
		}
	}

	newBinding := func(opts *options.HttpRule, idx int) (*Binding, error) {
		var (
			httpMethod   string
			pathTemplate string
		)
		switch {
		case opts.GetGet() != "":
			httpMethod = "GET"
			pathTemplate = opts.GetGet()
			if opts.Body != "" {
				return nil, fmt.Errorf("must not set request body when http method is GET: %s", md.GetName())
			}

		case opts.GetPut() != "":
			httpMethod = "PUT"
			pathTemplate = opts.GetPut()

		case opts.GetPost() != "":
			httpMethod = "POST"
			pathTemplate = opts.GetPost()

		case opts.GetDelete() != "":
			httpMethod = "DELETE"
			pathTemplate = opts.GetDelete()
			if opts.Body != "" && !r.allowDeleteBody {
				return nil, fmt.Errorf("must not set request body when http method is DELETE except allow_delete_body option is true: %s", md.GetName())
			}

		case opts.GetPatch() != "":
			httpMethod = "PATCH"
			pathTemplate = opts.GetPatch()

		case opts.GetCustom() != nil:
			custom := opts.GetCustom()
			httpMethod = custom.Kind
			pathTemplate = custom.Path

		default:
			if grpclog.V(1) {
				grpclog.Infof("No pattern specified in google.api.HttpRule: %s", md.GetName())
			}
			return nil, nil
		}

		parsed, err := httprule.Parse(pathTemplate)
		if err != nil {
			return nil, err
		}
		tmpl := parsed.Compile()

		if md.GetClientStreaming() && len(tmpl.Fields) > 0 {
			return nil, errors.New("cannot use path parameter in client streaming")
		}

		b := &Binding{
			Method:     meth,
			Index:      idx,
			PathTmpl:   tmpl,
			HTTPMethod: httpMethod,
		}

		for _, f := range tmpl.Fields {
			param, err := r.newParam(meth, f)
			if err != nil {
				return nil, err
			}
			b.PathParams = append(b.PathParams, param)
		}

		// TODO(yugui) Handle query params

		b.Body, err = r.newBody(meth, opts.Body)
		if err != nil {
			return nil, err
		}

		b.ResponseBody, err = r.newResponse(meth, opts.ResponseBody)
		if err != nil {
			return nil, err
		}

		return b, nil
	}

	applyOpts := func(opts *options.HttpRule) error {
		b, err := newBinding(opts, len(meth.Bindings))
		if err != nil {
			return err
		}

		if b != nil {
			meth.Bindings = append(meth.Bindings, b)
		}
		for _, additional := range opts.GetAdditionalBindings() {
			if len(additional.AdditionalBindings) > 0 {
				return fmt.Errorf("additional_binding in additional_binding not allowed: %s.%s", svc.GetName(), meth.GetName())
			}
			b, err := newBinding(additional, len(meth.Bindings))
			if err != nil {
				return err
			}
			meth.Bindings = append(meth.Bindings, b)
		}

		return nil
	}

	for _, opts := range optsList {
		if err := applyOpts(opts); err != nil {
			return nil, err
		}
	}

	return meth, nil
}

func extractRoleOptions(meth *descriptorpb.MethodDescriptorProto) (*myoptions.Role, error) {
	if meth.Options == nil {
		return nil, nil
	}
	if !proto.HasExtension(meth.Options, myoptions.E_Role) {
		return nil, nil
	}
	ext := proto.GetExtension(meth.Options, myoptions.E_Role)
	role, ok := ext.(*myoptions.Role)
	if !ok {
		return nil, fmt.Errorf("extension is %T; want a Role", ext)
	}

	// Validate Role fields for kebab-case format
	if err := validateKebabCase("resource", role.Resource); err != nil {
		return nil, fmt.Errorf("invalid role in method %s: %w", meth.GetName(), err)
	}
	if err := validateKebabCase("verb", role.Verb); err != nil {
		return nil, fmt.Errorf("invalid role in method %s: %w", meth.GetName(), err)
	}
	for i, scope := range role.Scope {
		if err := validateKebabCase(fmt.Sprintf("scope[%d]", i), scope); err != nil {
			return nil, fmt.Errorf("invalid role in method %s: %w", meth.GetName(), err)
		}
	}

	return role, nil
}

func extractAPIOptions(meth *descriptorpb.MethodDescriptorProto) (*options.HttpRule, error) {
	if meth.Options == nil {
		return nil, nil
	}
	if !proto.HasExtension(meth.Options, options.E_Http) {
		return nil, nil
	}
	ext := proto.GetExtension(meth.Options, options.E_Http)
	opts, ok := ext.(*options.HttpRule)
	if !ok {
		return nil, fmt.Errorf("extension is %T; want an HttpRule", ext)
	}
	return opts, nil
}

func defaultAPIOptions(svc *Service, md *descriptorpb.MethodDescriptorProto) (*options.HttpRule, error) {
	// FQSN prefixes the service's full name with a '.', e.g.: '.example.ExampleService'
	fqsn := strings.TrimPrefix(svc.FQSN(), ".")

	// This generates an HttpRule that matches the gRPC mapping to HTTP/2 described in
	// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md#requests
	// i.e.:
	//   * method is POST
	//   * path is "/<service name>/<method name>"
	//   * body should contain the serialized request message
	rule := &options.HttpRule{
		Pattern: &options.HttpRule_Post{
			Post: fmt.Sprintf("/%s/%s", fqsn, md.GetName()),
		},
		Body: "*",
	}
	return rule, nil
}

func (r *Registry) newParam(meth *Method, path string) (Parameter, error) {
	msg := meth.RequestType
	fields, err := r.resolveFieldPath(msg, path, true)
	if err != nil {
		return Parameter{}, err
	}
	l := len(fields)
	if l == 0 {
		return Parameter{}, fmt.Errorf("invalid field access list for %s", path)
	}
	target := fields[l-1].Target
	switch target.GetType() {
	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		if grpclog.V(2) {
			grpclog.Infoln("found aggregate type:", target, target.TypeName)
		}
		if IsWellKnownType(*target.TypeName) {
			if grpclog.V(2) {
				grpclog.Infoln("found well known aggregate type:", target)
			}
		} else {
			return Parameter{}, fmt.Errorf("%s.%s: %s is a protobuf message type. Protobuf message types cannot be used as path parameters, use a scalar value type (such as string) instead", meth.Service.GetName(), meth.GetName(), path)
		}
	}
	return Parameter{
		FieldPath: FieldPath(fields),
		Method:    meth,
		Target:    fields[l-1].Target,
	}, nil
}

func (r *Registry) newBody(meth *Method, path string) (*Body, error) {
	switch path {
	case "":
		return nil, nil
	case "*":
		return &Body{FieldPath: nil}, nil
	}
	msg := meth.RequestType
	fields, err := r.resolveFieldPath(msg, path, false)
	if err != nil {
		return nil, err
	}
	return &Body{FieldPath: FieldPath(fields)}, nil
}

func (r *Registry) newResponse(meth *Method, path string) (*Body, error) {
	msg := meth.ResponseType
	switch path {
	case "", "*":
		return nil, nil
	}
	fields, err := r.resolveFieldPath(msg, path, false)
	if err != nil {
		return nil, err
	}
	return &Body{FieldPath: FieldPath(fields)}, nil
}

// lookupField looks up a field named "name" within "msg".
// It returns nil if no such field found.
func lookupField(msg *Message, name string) *Field {
	for _, f := range msg.Fields {
		if f.GetName() == name {
			return f
		}
	}
	return nil
}

// resolveFieldPath resolves "path" into a list of fieldDescriptor, starting from "msg".
func (r *Registry) resolveFieldPath(msg *Message, path string, isPathParam bool) ([]FieldPathComponent, error) {
	if path == "" {
		return nil, nil
	}

	root := msg
	var result []FieldPathComponent
	for i, c := range strings.Split(path, ".") {
		if i > 0 {
			f := result[i-1].Target
			switch f.GetType() {
			case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, descriptorpb.FieldDescriptorProto_TYPE_GROUP:
				var err error
				msg, err = r.LookupMsg(msg.FQMN(), f.GetTypeName())
				if err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("not an aggregate type: %s in %s", f.GetName(), path)
			}
		}

		if grpclog.V(2) {
			grpclog.Infof("Lookup %s in %s", c, msg.FQMN())
		}
		f := lookupField(msg, c)
		if f == nil {
			return nil, fmt.Errorf("no field %q found in %s", path, root.GetName())
		}
		if isPathParam && f.GetProto3Optional() {
			return nil, fmt.Errorf("optional field not allowed in field path: %s in %s", f.GetName(), path)
		}
		result = append(result, FieldPathComponent{Name: c, Target: f})
	}
	return result, nil
}
