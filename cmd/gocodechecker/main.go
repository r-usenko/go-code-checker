package main

import (
	"flag"
	"log"
	"os/exec"
	"path/filepath"
	"time"

	codeChecker "github.com/r-usenko/go-code-checker"
)

var dir = flag.String("dir", ".", "root directory with go.mod")

var tidy = flag.Bool("tidy", false, "use go mod tidy")

var imp = flag.Bool("imports", false, "use goimports")
var impPrefix = flag.String("imports-prefix", "", "group by module prefix")

var write = flag.Bool("write", false, "apply changes to file")

func main() {
	flag.Parse()

	var opts = []codeChecker.Option{
		codeChecker.WithLogger(log.Default()),
	}
	var binary string
	var err error

	absDir, _ := filepath.Abs(*dir)
	log.Printf("PROCESS DIRECTORY: [%s]\n", absDir)

	if *tidy {
		binary, err = exec.LookPath(codeChecker.BinaryGo)
		if err != nil {
			log.Panic(err)
		}
		log.Printf("USE: [%s mod tidy] group and sort (require)\n", binary)
		opts = append(opts, codeChecker.WithJoinRequireModules())
	}

	if *imp || *impPrefix != "" {
		binary, err = exec.LookPath(codeChecker.BinaryGoImports)
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

	var dur time.Duration
	dur, err = codeChecker.New(*dir, opts...).Run(*write)

	if err != nil {
		log.Panic(err)
	}

	log.Printf("DURATION: %s", dur)
}
