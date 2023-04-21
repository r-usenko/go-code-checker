package codeChecker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

	BinaryGoImports             = "goimports"
	binaryGoImportsParamVerbose = "-v"
	binaryGoImportsParamList    = "-l"
	binaryGoImportsParamWrite   = "-w"
	binaryGoImportsParamLocal   = "-local"
	binaryGoImportsParamFormat  = "--format-only"
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
		args = append(args, []string{binaryGoImportsParamLocal, fmt.Sprintf(`"%s"`, m.localPrefix)}...)
	}

	outCh := make(chan struct {
		prefix string
		data   []byte
	})
	errCh := make(chan error)
	wg := sync.WaitGroup{}
	wg.Add(1)

	defer func() {
		wg.Wait()
		close(outCh)
		close(errCh)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var files []string

	go func() {
		defer func() {
			wg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case p := <-outCh:
				if args[0] == binaryGoImportsParamList {
					files = append(files, string(p.data))
				}

				m.logger.Printf("PROCESS [%s]: %s", p.prefix, p.data)
			case errVal := <-errCh:
				_ = errVal //TODO EOF, ExitStatus
				//m.logger.Println("ERROR:", errVal)

			default:
				time.Sleep(time.Millisecond)
			}
		}
	}()

	//Get list for files
	args = append(args, []string{
		binaryGoImportsParamFormat,
		//binaryGoImportsParamVerbose,
		dir,
	}...)

	m.runGoImport("LIST", outCh, errCh, dir, args)

	if len(files) == 0 {
		return
	}

	args[0] = binaryGoImportsParamWrite //replace list to write flag
	lastArgIndex := len(args) - 1

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

	//First preprocess
	m.runGoImport("GROUP", outCh, errCh, dir, args) //TODO we cant separate parse and save errors (this changes will not been reverted)
	after := getFiles("", files)

	for _, fi := range after {
		args[lastArgIndex] = fi.filename //overwrite filename
		fi.data = reImport.ReplaceAllFunc(fi.data, func(data []byte) []byte {
			return reComment.ReplaceAll(data, nil)
		})
		fi.data = reImport.ReplaceAllFunc(fi.data, func(data []byte) []byte {
			return reEmptyLines.ReplaceAll(data, nil)
		})

		if err = os.WriteFile(fi.filename, fi.data, fi.info.Mode()); err != nil {
			return //Rollback
		}
	}

	m.runGoImport("SORT", outCh, errCh, dir, args) //TODO we cant separate parse and save errors (this changes will not been reverted)

	return
}

func (m *module) runGoImport(prefix string, outCh chan struct {
	prefix string
	data   []byte
}, errCh chan error, dir string, args []string) {
	cmd := exec.Command(BinaryGoImports, args...)
	cmd.Dir = dir

	ctx, cancel := context.WithCancel(context.Background())

	var err error
	var pOut io.ReadCloser

	defer func() {
		_ = pOut.Close()
		cancel()
		errCh <- err
	}()

	pOut, err = cmd.StdoutPipe()
	if err != nil {
		return
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	go func() {
		var n int
		var buff = make([]byte, 1024)
		var errPipe error
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, errPipe = pOut.Read(buff)
				if errPipe != nil {
					errCh <- errPipe
				}
				outCh <- struct {
					prefix string
					data   []byte
				}{
					prefix: prefix,
					data:   buff[:n],
				}
			}
		}
	}()
	if err = cmd.Start(); err != nil {
		return
	}

	//Wait Command
	if err = cmd.Wait(); err != nil {
		return
	}

	//Wait stop sending
	cancel()
	wg.Wait()
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
