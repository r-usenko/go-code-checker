package codeChecker

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/modfile"
)

const (
	GoMod     = "go.mod"
	GoModSum  = "go.sum"
	GoWork    = "go.work"
	GoWorkSum = "go.work.sum"
)

const (
	BinaryGo          = "go"
	binaryGoParamMod  = "mod"
	binaryGoParamTidy = "tidy"

	BinaryGoImports            = "goimports"
	binaryGoImportsParamList   = "-l"
	binaryGoImportsParamWrite  = "-w"
	binaryGoImportsParamLocal  = "-local"
	binaryGoImportsParamFormat = "--format-only"
)

var ErrDetected = errors.New("changes detected")

var muMutex sync.Mutex

type fData struct {
	filename string
	info     os.FileInfo
	data     []byte
}

var (
	reImport     = regexp.MustCompile(`(?sm)^import\s\(\n(.*?)\)`)
	reComment    = regexp.MustCompile(`\s(//[^\n]+)|(/\*[\s\S]*?\*/)`)
	reEmptyLines = regexp.MustCompile(`(?sm)^\s*\n`)
)

func getFiles(dir string, files []string) map[string]fData {
	var results = make(map[string]fData)

	if dir != "" {
		dir = "/" + dir
	}

	for _, file := range files {
		filename := dir + file

		fi, err := os.Lstat(filename)
		if err != nil {
			if _, ok := err.(*fs.PathError); ok {
				continue
			}

			panic(err)
		}

		data, err := os.ReadFile(filename)
		if err != nil {
			panic(err)
		}

		results[file] = fData{
			filename: filename,
			info:     fi,
			data:     data,
		}
	}

	return results
}

func (m *module) joinRequire(isWrite bool) (err error) {
	muMutex.Lock()
	defer muMutex.Unlock()

	dir, err := filepath.Abs(m.path)
	if err != nil {
		return err
	}

	var fileList = []string{
		GoMod,
		GoModSum,
		GoWork,
		GoWorkSum,
	}

	filesBefore := getFiles(dir, fileList)

	fi, ok := filesBefore[GoMod]
	if !ok {
		return errors.New("missing go.mod in " + dir)
	}

	f, err := modfile.Parse(fi.filename, fi.data, nil)
	if err != nil {
		return err
	}

	for _, r := range f.Require {
		copyMod := r.Mod
		if copyMod.Path == "" {
			continue
		}

		err = f.DropRequire(copyMod.Path)
		if err != nil {
			return err
		}

		err = f.AddRequire(copyMod.Path, copyMod.Version)
		if err != nil {
			return err
		}
	}

	f.Cleanup()
	f.SortBlocks()

	modified, err := f.Format()
	if err != nil {
		return err
	}

	//TODO How to check without modify files?

	//Save modified for tidy
	err = os.WriteFile(fi.filename, modified, fi.info.Mode())
	if err != nil {
		return err
	}

	defer func() {
		//If we had error on save modified or go mod tidy, rollback all affected files
		if err != nil {
			isWrite = false
		}

		//We don't have error in write scenario, rollback don't needed
		if isWrite {
			return
		}

		//Rollback
		filesAfter := getFiles(dir, fileList)

		if len(filesAfter) != len(filesBefore) {
			err = errors.Join(err, ErrDetected)
		}

		var fileAfter fData
		for name, fileBefore := range filesBefore {
			//Rollback and join errors
			err = errors.Join(err, os.WriteFile(fileBefore.filename, fileBefore.data, fileBefore.info.Mode()))

			fileAfter, ok = filesAfter[name]
			if !ok {
				err = errors.Join(err, ErrDetected)
				continue
			}

			if bytes.Compare(fileAfter.data, fileBefore.data) != 0 {
				err = errors.Join(err, ErrDetected)
				continue
			}
		}
	}()

	//Call Tidy
	cmd := exec.Command(BinaryGo, binaryGoParamMod, binaryGoParamTidy)
	cmd.Dir = dir
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func (m *module) formatImport(isWrite bool) (err error) {
	muMutex.Lock()
	defer muMutex.Unlock()

	dir, err := filepath.Abs(m.path)
	if err != nil {
		return
	}

	var args = []string{binaryGoImportsParamList} //list only

	if m.localPrefix != "" {
		args = append(args, []string{binaryGoImportsParamLocal, m.localPrefix}...)
	}

	//Get list for files
	args = append(args, []string{binaryGoImportsParamFormat, dir}...)
	out, err := runGoImport(dir, args)
	if err != nil {
		return
	}

	args[0] = binaryGoImportsParamWrite //replace list to write flag
	lastArgIndex := len(args) - 1
	files := strings.Split(out, "\n")
	processed := getFiles("", files)

	defer func() {
		//If we had error on save modified or go mod tidy, rollback all affected files
		if err != nil {
			isWrite = false
		}

		//We don't have error in write scenario, rollback don't needed
		if isWrite {
			return
		}

		//Rollback and join errors
		for _, fi := range processed {
			err = errors.Join(err, os.WriteFile(fi.filename, fi.data, fi.info.Mode()))
		}
	}()

	for _, fi := range processed {
		var newFileData = make([]byte, len(fi.data))
		copy(newFileData, fi.data)

		args[lastArgIndex] = fi.filename //overwrite filename

		if _, err = runGoImport(dir, args); err != nil {
			return //Rollback
		}

		newFileData = reImport.ReplaceAllFunc(newFileData, func(data []byte) []byte {
			return reComment.ReplaceAll(data, nil)
		})
		newFileData = reImport.ReplaceAllFunc(newFileData, func(data []byte) []byte {
			return reEmptyLines.ReplaceAll(data, nil)
		})

		if err = os.WriteFile(fi.filename, newFileData, fi.info.Mode()); err != nil {
			return //Rollback
		}

		//We need run goimports again for sort and group without empty lines
		if _, err = runGoImport(dir, args); err != nil {
			return //Rollback
		}
	}

	return
}

func runGoImport(dir string, args []string) (out string, err error) {
	cmd := exec.Command(BinaryGoImports, args...)
	cmd.Dir = dir

	cmd.Stdout = new(strings.Builder)
	cmd.Stderr = new(strings.Builder)

	if err = cmd.Run(); err == nil {
		out = cmd.Stdout.(*strings.Builder).String()
		return
	}

	if erc, ok := err.(*exec.ExitError); ok && erc.ExitCode() != 0 {
		err = errors.New(cmd.Stderr.(*strings.Builder).String())
	}

	return
}

func (m *module) Run(state bool) (duration time.Duration, err error) {
	start := time.Now()

	if m.withJoin {
		if err = m.joinRequire(state); err != nil {
			return
		}
	}

	if m.withImports {
		if err = m.formatImport(state); err != nil {
			return
		}
	}

	duration = time.Since(start)
	return
}

//goland:noinspection GoExportedFuncWithUnexportedType
func New(path string, opts ...Option) *module {
	m := &module{
		path: path,
	}
	m.apply(opts)

	return m
}
