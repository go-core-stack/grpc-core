// Copyright © 2025 Prabhjot Singh Sethi, All Rights reserved
// Author: Prabhjot Singh Sethi <prabhjot.sethi@gmail.com>

// Command protoc-gen-sdk is a plugin for Google protocol buffer
// compiler to generate Go SDK wrapper code for gRPC services.
// This tool automatically generates SDK client libraries from protobuf
// service definitions, including authentication, timeout handling, and
// helper methods for JSON parsing.
//
// You rarely need to run this program directly. Instead, put this program
// into your $PATH with a name "protoc-gen-sdk" and run
//
//	protoc --sdk_out=output_directory path/to/input.proto
//
// The generated code will be placed in files with the .pb.sdk.go suffix.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/protobuf/compiler/protogen"

	"github.com/go-core-stack/grpc-core/internal/codegenerator"
	"github.com/go-core-stack/grpc-core/internal/descriptor"
	"github.com/go-core-stack/grpc-core/protoc-gen-sdk/internal/gensdk"
)

var (
	omitPackageDoc = flag.Bool("omit_package_doc", false, "if true, no package comment will be included in the generated code")
	standalone     = flag.Bool("standalone", false, "generates a standalone SDK package, which imports the target service package")
	versionFlag    = flag.Bool("version", false, "print the current version")
)

// Variables set by goreleaser at build time
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	flag.Parse()

	if *versionFlag {
		if commit == "unknown" {
			buildInfo, ok := debug.ReadBuildInfo()
			if ok {
				version = buildInfo.Main.Version
				for _, setting := range buildInfo.Settings {
					if setting.Key == "vcs.revision" {
						commit = setting.Value
					}
					if setting.Key == "vcs.time" {
						date = setting.Value
					}
				}
			}
		}
		fmt.Printf("Version %v, commit %v, built at %v\n", version, commit, date)
		os.Exit(0)
	}

	protogen.Options{
		ParamFunc: flag.CommandLine.Set,
	}.Run(func(gen *protogen.Plugin) error {
		reg := descriptor.NewRegistry()

		if err := applyFlags(reg); err != nil {
			return err
		}

		codegenerator.SetSupportedFeaturesOnPluginGen(gen)

		generator := gensdk.New(reg, *standalone)

		if grpclog.V(1) {
			grpclog.Infof("Parsing code generator request")
		}

		if err := reg.LoadFromPlugin(gen); err != nil {
			return err
		}

		targets := make([]*descriptor.File, 0, len(gen.Request.FileToGenerate))
		for _, target := range gen.Request.FileToGenerate {
			f, err := reg.LookupFile(target)
			if err != nil {
				return err
			}
			targets = append(targets, f)
		}

		files, err := generator.Generate(targets)
		for _, f := range files {
			if grpclog.V(1) {
				grpclog.Infof("NewGeneratedFile %q in %s", f.GetName(), f.GoPkg)
			}

			genFile := gen.NewGeneratedFile(f.GetName(), protogen.GoImportPath(f.GoPkg.Path))
			if _, err := genFile.Write([]byte(f.GetContent())); err != nil {
				return err
			}
		}

		if grpclog.V(1) {
			grpclog.Info("Processed code generator request")
		}

		return err
	})
}

func applyFlags(reg *descriptor.Registry) error {
	reg.SetStandalone(*standalone)
	reg.SetOmitPackageDoc(*omitPackageDoc)
	return nil
}
