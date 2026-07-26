package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mirkobrombin/go-foundation/dev/v2/analyzer"
	"github.com/mirkobrombin/go-foundation/dev/v2/generator"
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
	case "version":
		fmt.Println("foundation dev v2")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: foundation <check|generate|version> [arguments]")
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
