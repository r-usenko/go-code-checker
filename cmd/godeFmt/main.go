package main

import (
	"flag"
	"log"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/r-usenko/godeFmt"
)

var dir = flag.String("dir", ".", "root directory with go.mod")

var tidy = flag.Bool("tidy", false, "use go mod tidy")

var imp = flag.Bool("imports", false, "use goimports")
var impPrefix = flag.String("imports-prefix", "", "group by module prefix")

var write = flag.Bool("write", false, "apply changes to file")

func main() {
	flag.Parse()

	var opts = []godeFmt.Option{
		godeFmt.WithLogger(log.Default()),
	}
	var binary string
	var err error

	absDir, _ := filepath.Abs(*dir)
	log.Printf("PROCESS DIRECTORY: [%s]\n", absDir)

	if *tidy {
		binary, err = exec.LookPath(godeFmt.BinaryGo)
		if err != nil {
			log.Panic(err)
		}
		log.Printf("USE: [%s mod tidy] group and sort (require)\n", binary)
		opts = append(opts, godeFmt.WithJoinRequireModules())
	}

	if *imp || *impPrefix != "" {
		binary, err = exec.LookPath(godeFmt.BinaryGoImports)
		if err != nil {
			log.Panic(err)
		}
		log.Printf("USE: [%s] group and sort\n", binary)
		opts = append(opts, godeFmt.WithImports())
	}

	if *impPrefix != "" {
		log.Printf("USE: [%s] group by repository prefix (%s)\n", binary, *impPrefix)
		opts = append(opts, godeFmt.WithLocalPrefix(*impPrefix))
	}

	var dur time.Duration
	dur, err = godeFmt.New(*dir, opts...).Run(*write)

	if err != nil {
		log.Panic(err)
	}

	log.Printf("DURATION: %s", dur)
}
