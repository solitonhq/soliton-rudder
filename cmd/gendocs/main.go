// Command gendocs regenerates documentation derived from code.
//
//	go run ./cmd/gendocs          # write docs/ASSESSMENT-RULES.md
//	go run ./cmd/gendocs -check   # exit 1 if the file is out of date (used in CI)
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"soliton-rudder/internal/feasibility"
)

func main() {
	check := flag.Bool("check", false, "verify instead of writing")
	flag.Parse()
	path := filepath.Join("docs", "ASSESSMENT-RULES.md")
	want := feasibility.RulesMarkdown()
	if *check {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			fmt.Fprintf(os.Stderr, "%s is out of date; run `go run ./cmd/gendocs`\n", path)
			os.Exit(1)
		}
		return
	}
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("wrote", path)
}
