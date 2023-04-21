package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/r-usenko/godeFmt"
)

var dir = flag.String("dir", ".", "Root directory with go.mod")
var tidy = flag.Bool("tidy", false, "Use 'go mod tidy'")
var imp = flag.Bool("imports", false, "Use 'goimports'")
var impPrefix = flag.String("imports-prefix", "", "Use 'goimports' and group by module prefix")
var write = flag.Bool("write", false, "Applies changes to files. Otherwise, an attempt will be made to recover the files if a formatter error is can be detected")
var _ = flag.Bool("version", false, "Print version")

func main() {
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	switch flag.Arg(0) {
	case "version", "-version", "--version":
		fmt.Println(godeFmt.Version)
		os.Exit(0)
	default:
		var exists bool
		flag.VisitAll(func(f *flag.Flag) {
			if flag.Arg(0) == f.Name {
				exists = true
			}
		})
		if !exists {
			flag.Usage()
			os.Exit(2)
		}
	}

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
