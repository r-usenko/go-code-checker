package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

func main() {
	if len(os.Args) != 2 {
		panic(errors.New("invalid argument with version"))
	}

	if !regexp.MustCompile(`^v\.\d+\.\d+\.\d+$`).MatchString(os.Args[1]) {
		panic(fmt.Errorf("invalid version %q", os.Args[1]))
	}

	_, file, _, _ := runtime.Caller(0)
	file, _ = filepath.Abs(filepath.Dir(file) + "/../version.go")

	if err := os.WriteFile(file, []byte(fmt.Sprintf(`package godeFmt
//go:generate go run ./cmd/set-version.go ${GITHUB_REF_NAME}
var Version = "%s"`, os.Args[1])), 0644); err != nil {
		panic(err)
	}
}
