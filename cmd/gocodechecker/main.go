package main

import (
	"flag"
	"log"
	"os/exec"
	"path/filepath"

	codeChecker "github.com/r-usenko/go-code-checker"
)

var dir = flag.String("dir", ".", "root directory with go.mod")

var tidy = flag.Bool("tidy", true, "use go mod tidy")
var imp = flag.Bool("imports", false, "use goimports")
var impPrefix = flag.String("imports-prefix", "", "group by module prefix")

const (
	binaryGo        = "go"
	binaryGoImports = "goimports"
)

func main() {
	flag.Parse()

	var opts []codeChecker.Option
	var binary string
	var err error

	absDir, _ := filepath.Abs(*dir)
	log.Printf("PROCESS DIRECTORY: [%s]\n", absDir)

	if *tidy {
		binary, err = exec.LookPath(binaryGo)
		if err != nil {
			log.Panic(err)
		}
		log.Printf("USE: [%s mod tidy] group and sort (require)\n", binary)
		opts = append(opts, codeChecker.WithJoinRequireModules())
	}

	if *imp || *impPrefix != "" {
		binary, err = exec.LookPath(binaryGoImports)
		if err != nil {
			log.Panic(err)
		}
		log.Printf("USE: [%s] group and sort\n", binary)
		opts = append(opts, codeChecker.WithImports())
	}

	if *impPrefix != "" {
		log.Printf("USE: [%s] group by repository prefix (%s)\n", binary, *impPrefix)
		opts = append(opts, codeChecker.WithLocalPrefix(*impPrefix))
	}

	if err = codeChecker.New(*dir, opts...).Run(); err != nil {
		log.Panic(err)
	}
}
