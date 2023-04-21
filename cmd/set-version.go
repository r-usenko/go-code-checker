package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	_, file, _, _ := runtime.Caller(0)
	file, _ = filepath.Abs(filepath.Dir(file) + "/../version.go")
	fmt.Println(file, os.Args[1])
	//os.WriteFile("")
}
