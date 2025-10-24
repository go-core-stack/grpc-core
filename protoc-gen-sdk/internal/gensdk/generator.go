package gensdk

import (
	"errors"
	"fmt"
	"go/format"
	"path"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/go-core-stack/grpc-core/internal/descriptor"
	gen "github.com/go-core-stack/grpc-core/internal/generator"
)

var errNoTargetService = errors.New("no target service defined in the file")

type generator struct {
	reg                *descriptor.Registry
	imports            []descriptor.GoPackage
	useRequestContext  bool
	registerFuncSuffix string
	allowPatchFeature  bool
	standalone         bool
}

func UpdateReserveGoImports(reg *descriptor.Registry, packages []string) []descriptor.GoPackage {
	var imports []descriptor.GoPackage
	for _, pkgpath := range packages {
		pkg := descriptor.GoPackage{
			Path: pkgpath,
			Name: path.Base(pkgpath),
		}
		if err := reg.ReserveGoPackageAlias(pkg.Name, pkg.Path); err != nil {
			for i := 0; ; i++ {
				alias := fmt.Sprintf("%s_%d", pkg.Name, i)
				if err := reg.ReserveGoPackageAlias(alias, pkg.Path); err != nil {
					continue
				}
				pkg.Alias = alias
				break
			}
		}
		imports = append(imports, pkg)
	}
	return imports
}

// New returns a new generator which generates grpc gateway files.
func New(reg *descriptor.Registry, useRequestContext bool, registerFuncSuffix string,
	allowPatchFeature, standalone bool) gen.Generator {
	imports := UpdateReserveGoImports(reg, []string{
		"io",
		"net/http",
		"github.com/go-core-stack/auth/client",
		"github.com/grpc-ecosystem/grpc-gateway/v2/runtime",
	})
	return &generator{
		reg:                reg,
		imports:            imports,
		useRequestContext:  useRequestContext,
		registerFuncSuffix: registerFuncSuffix,
		allowPatchFeature:  allowPatchFeature,
		standalone:         standalone,
	}
}

func (g *generator) Generate(targets []*descriptor.File) ([]*descriptor.ResponseFile, error) {
	var files []*descriptor.ResponseFile
	for _, file := range targets {
		if grpclog.V(1) {
			grpclog.Infof("Processing %s", file.GetName())
		}

		code, err := g.generate(file)
		if err == errNoTargetService {
			if grpclog.V(1) {
				grpclog.Infof("%s: %v", file.GetName(), err)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		formatted, err := format.Source([]byte(code))
		if err != nil {
			grpclog.Errorf("%v: %s", err, code)
			return nil, err
		}
		files = append(files, &descriptor.ResponseFile{
			GoPkg: file.GoPkg,
			CodeGeneratorResponse_File: &pluginpb.CodeGeneratorResponse_File{
				Name:    proto.String(file.GeneratedFilenamePrefix + ".sdk.go"),
				Content: proto.String(string(formatted)),
			},
		})
	}
	return files, nil
}

func (g *generator) generate(file *descriptor.File) (string, error) {
	pkgSeen := make(map[string]bool)
	var imports []descriptor.GoPackage
	for _, pkg := range g.imports {
		pkgSeen[pkg.Path] = true
		imports = append(imports, pkg)
	}

	if g.standalone {
		imports = append(imports, file.GoPkg)
	}

	hasQueryParams := false
	hasPathParams := false
	includeHeader4Body := false
	for _, svc := range file.Services {
		for _, m := range svc.Methods {
			pkg := m.RequestType.File.GoPkg
			if len(m.Bindings) != 0 {
				b := m.Bindings[0]
				if b.Body != nil {
					includeHeader4Body = true
				}
				if HasQueryParam(b) {
					hasQueryParams = true
				}
				if len(b.PathParams) != 0 {
					hasPathParams = true
				}
			}
			if len(m.Bindings) == 0 ||
				pkg == file.GoPkg || pkgSeen[pkg.Path] {
				continue
			}
			pkgSeen[pkg.Path] = true
			imports = append(imports, pkg)
		}
	}
	if includeHeader4Body {
		newImports := UpdateReserveGoImports(g.reg, []string{"bytes"})
		imports = append(imports, newImports...)
	}
	if hasQueryParams || hasPathParams {
		requiredImports := []string{
			"fmt",
		}
		if hasQueryParams {
			requiredImports = append(requiredImports, "net/url")
		}
		if hasPathParams {
			requiredImports = append(requiredImports, "strings")
		}
		newImports := UpdateReserveGoImports(g.reg, requiredImports)
		imports = append(imports, newImports...)
	}

	params := param{
		File:               file,
		Imports:            imports,
		UseRequestContext:  g.useRequestContext,
		RegisterFuncSuffix: g.registerFuncSuffix,
		AllowPatchFeature:  g.allowPatchFeature,
	}
	if g.reg != nil {
		params.OmitPackageDoc = g.reg.GetOmitPackageDoc()
	}
	return applyTemplate(params, g.reg)
}
