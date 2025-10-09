package gensdk

import (
	"errors"
	"go/format"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/go-core-stack/grpc-core/internal/descriptor"
	gen "github.com/go-core-stack/grpc-core/internal/generator"
)

var errNoTargetService = errors.New("no target service defined in the file")

type generator struct {
	reg        *descriptor.Registry
	standalone bool
}

// New returns a new generator which generates Go SDK wrapper files.
func New(reg *descriptor.Registry, standalone bool) gen.Generator {
	return &generator{
		reg:        reg,
		standalone: standalone,
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
				Name:    proto.String(file.GeneratedFilenamePrefix + ".pb.sdk.go"),
				Content: proto.String(string(formatted)),
			},
		})
	}
	return files, nil
}

func (g *generator) generate(file *descriptor.File) (string, error) {
	params := param{
		File: file,
	}
	if g.reg != nil {
		params.OmitPackageDoc = g.reg.GetOmitPackageDoc()
	}
	return applyTemplate(params, g.reg)
}
