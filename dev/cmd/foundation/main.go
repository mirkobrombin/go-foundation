package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mirkobrombin/go-foundation/dev/v2/analyzer"
	"github.com/mirkobrombin/go-foundation/dev/v2/catalog"
	"github.com/mirkobrombin/go-foundation/dev/v2/generator"
	"github.com/mirkobrombin/go-foundation/dev/v2/mcp"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "check":
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		singlechecker.Main(analyzer.Analyzer)
	case "generate":
		generate(os.Args[2:])
	case "catalog":
		buildCatalog(os.Args[2:])
	case "mcp":
		serveMCP(os.Args[2:])
	case "version":
		fmt.Println("foundation dev v2")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: foundation <check|generate|mcp|catalog|version> [arguments]")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  check     report Foundation declaration and wiring problems")
	fmt.Fprintln(os.Stderr, "  generate  write static registries and contract assertions")
	fmt.Fprintln(os.Stderr, "  mcp       serve the Model Context Protocol server on stdio")
	fmt.Fprintln(os.Stderr, "  catalog   regenerate the embedded API catalog (-check to verify)")
	fmt.Fprintln(os.Stderr, "  version   print the tool version")
}

func generate(args []string) {
	check := false
	var patterns []string
	for _, arg := range args {
		if arg == "-check" {
			check = true
			continue
		}
		patterns = append(patterns, arg)
	}
	if check {
		if err := generator.Check(context.Background(), ".", patterns...); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	paths, err := generator.Write(context.Background(), ".", patterns...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, path := range paths {
		fmt.Println(path)
	}
}

// buildCatalog regenerates the API catalog the MCP server answers from. It runs
// inside the development module and reads the runtime module beside it, so it is
// a maintainer command rather than something an application needs.
func buildCatalog(args []string) {
	check := false
	devRoot := "."
	runtimeRoot := ".."
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "-check":
			check = true
		case "-dev":
			index++
			if index < len(args) {
				devRoot = args[index]
			}
		case "-runtime":
			index++
			if index < len(args) {
				runtimeRoot = args[index]
			}
		}
	}

	if check {
		if err := catalog.Check(context.Background(), devRoot, runtimeRoot); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	path, err := catalog.Write(context.Background(), devRoot, runtimeRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(path)
}

func serveMCP(args []string) {
	workspace := "."
	for index := 0; index < len(args); index++ {
		if args[index] == "-workspace" {
			index++
			if index < len(args) {
				workspace = args[index]
			}
		}
	}
	if err := mcp.Serve(context.Background(), workspace); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
