package godeFmt

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func (m *module) getFiles(dir string, files []string) map[string]fData {
	var results = make(map[string]fData)

	if dir != "" {
		dir = dir + "/"
	}

	for _, file := range files {
		filename := dir + file

		fi, err := os.Lstat(filename)
		if err != nil {
			if _, ok := err.(*fs.PathError); ok {
				continue
			}

			m.logger.Panic(err)
		}

		data, err := os.ReadFile(filename)
		if err != nil {
			m.logger.Panic(err)
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

	filesBefore := m.getFiles(dir, fileList)

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
		filesAfter := m.getFiles(dir, fileList)

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

type messageChan struct {
	prefix  string
	data    []byte
	isError bool
}

func (m *module) formatImport(isWrite bool) (err error) {
	muMutex.Lock()
	defer muMutex.Unlock()

	var dir string
	dir, err = filepath.Abs(m.path)
	if err != nil {
		return
	}

	var srcFiles []string
	err = filepath.Walk(dir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if filepath.Ext(info.Name()) == ".go" {
				srcFiles = append(srcFiles, path)
			}
			return nil
		})
	if err != nil {
		return
	}

	srcProcessed := m.getFiles("", srcFiles)

	for _, fi := range srcProcessed {
		m.logger.Printf("REPLACE [ALL]: %s", fi.filename)

		newData := reImport.ReplaceAllFunc(fi.data, func(data []byte) []byte {
			return reEmptyLines.ReplaceAll(data, nil)
		})
		if err = os.WriteFile(fi.filename, newData, fi.info.Mode()); err != nil {
			return //TODO Rollback
		}
	}

	var args = []string{binaryGoImportsParamList} //list only

	if m.localPrefix != "" {
		args = append(args, []string{binaryGoImportsParamLocal, m.localPrefix}...)
	}

	outCh := make(chan messageChan)
	errCh := make(chan messageChan)
	wg := sync.WaitGroup{}
	wg.Add(1)

	defer func() {
		wg.Wait()
		close(outCh)
		close(errCh)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var listOutput string
	var errCmd error

	go func() {
		defer func() {
			wg.Done()
		}()
		for {
			var proc bool

			select {
			case <-ctx.Done():
				return
			case p := <-outCh:
				proc = true
				if p.isError {
					m.logger.Printf("WTF [%s]:\n%s", p.prefix, p.data)
				} else {
					if args[0] == binaryGoImportsParamList {
						listOutput += string(p.data)
					}
					m.logger.Printf("PROCESS [%s]: %s", p.prefix, p.data)
				}
			default:
			}

			select {
			case <-ctx.Done():
				return
			case errVal := <-errCh:
				proc = true
				_ = errVal
				//m.logger.Printf("ERROR [%s]:\n%s", errVal.prefix, errVal.data)
			default:
			}

			if !proc {
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

	errCmd = m.callStart("LIST", outCh, errCh, dir, args)

	files := strings.Split(strings.TrimSpace(listOutput), "\n")
	m.logger.Printf("COUNT [LIST]: %d", len(files))
	if len(files) == 0 {
		return
	}

	args[0] = binaryGoImportsParamWrite //replace list to write flag
	processed := m.getFiles("", files)

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
	//errCmd = m.callStart("GROUP", outCh, errCh, dir, args) //TODO we cant separate parse and save errors (this changes will not been reverted)
	//after := getFiles("", files)

	for _, fi := range processed {
		m.logger.Printf("REPLACE [GROUP]: %s", fi.filename)

		newData := reImport.ReplaceAllFunc(fi.data, func(data []byte) []byte {
			return reComment.ReplaceAll(data, nil)
		})
		newData = reImport.ReplaceAllFunc(newData, func(data []byte) []byte {
			return reEmptyLines.ReplaceAll(data, nil)
		})
		if err = os.WriteFile(fi.filename, newData, fi.info.Mode()); err != nil {
			return //Rollback
		}
	}

	errCmd = m.callStart("SORT", outCh, errCh, dir, args) //TODO we cant separate parse and save errors (this changes will not been reverted)

	_ = errCmd //TODO ignore exit error
	return
}

func readBuff(pipe io.ReadCloser, buff []byte, buffSize int) (message []byte, errOut error) {
	var n int
	for {
		n, errOut = pipe.Read(buff)
		if errOut != nil {
			return
		}

		message = append(message, buff[:n]...)
		if buffSize > n {
			return
		}
	}
}

func (m *module) callOutput(prefix string, outCh, errCh chan messageChan, dir string, args []string) (onExit error) {
	cmd := exec.Command(BinaryGoImports, args...)
	cmd.Dir = dir

	m.logger.Println()
	m.logger.Printf("RUN [%s]: %s", prefix, cmd.String())

	var data []byte
	data, onExit = cmd.Output()

	err := onExit.(*exec.ExitError)

	errCh <- messageChan{
		prefix:  prefix,
		data:    err.Stderr,
		isError: true,
	}
	outCh <- messageChan{
		prefix:  prefix,
		data:    data,
		isError: false,
	}

	return
}

func (m *module) callRun(prefix string, outCh, errCh chan messageChan, dir string, args []string) (onExit error) {
	cmd := exec.Command(BinaryGoImports, args...)
	cmd.Dir = dir

	m.logger.Println()
	m.logger.Printf("RUN [%s]: %s", prefix, cmd.String())

	var err, out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &err
	defer func() {
		errCh <- messageChan{
			prefix:  prefix,
			data:    []byte(err.String()),
			isError: true,
		}
		outCh <- messageChan{
			prefix:  prefix,
			data:    []byte(out.String()),
			isError: false,
		}
	}()

	onExit = cmd.Run()

	return
}

func (m *module) callStart(prefix string, outCh, errCh chan messageChan, dir string, args []string) (onExit error) {
	cmd := exec.Command(BinaryGoImports, args...)
	cmd.Dir = dir

	m.logger.Println()
	m.logger.Printf("RUN [%s]: %s", prefix, cmd.String())

	var err error
	var pOut io.ReadCloser
	var pErr io.ReadCloser

	defer func() {
		_ = pOut.Close()
		_ = pErr.Close()
		onExit = err
	}()

	pOut, err = cmd.StdoutPipe()
	if err != nil {
		return
	}
	pErr, err = cmd.StderrPipe()
	if err != nil {
		return
	}

	wgReaderEnd := sync.WaitGroup{}
	wgReaderEnd.Add(1)

	var stopReadSignalErr = make(chan struct{})
	var stopReadSignal = make(chan struct{})

	bufferSize := 1024

	go func() {
		var buff = make([]byte, bufferSize)
		var errOut error
		var data []byte

		defer wgReaderEnd.Done()

		for {
			select {
			case <-stopReadSignal:
				return
			case <-stopReadSignalErr:
				return
			default:
				data, errOut = readBuff(pOut, buff, bufferSize)
				if errOut != nil {
					time.Sleep(time.Millisecond)
					continue
				}

				//Data exist, send it to channel
				outCh <- messageChan{
					prefix: prefix,
					data:   data,
				}
			}
		}
	}()

	go func() {
		var buff = make([]byte, bufferSize)
		var errOut error
		var data []byte

		defer wgReaderEnd.Done()

		for {
			select {
			case <-stopReadSignal:
				return
			case <-stopReadSignalErr:
				return
			default:
				data, errOut = readBuff(pErr, buff, bufferSize)
				if errOut != nil {
					time.Sleep(time.Millisecond)
					continue
				}

				//Data exist, send it to channel
				errCh <- messageChan{
					isError: true,
					prefix:  prefix,
					data:    data,
				}
			}
		}
	}()

	if err = cmd.Start(); err != nil {
		return
	}

	//Wait Command Finished
	if err = cmd.Wait(); err != nil {
		//exitCode := err.(*exec.ExitError).ExitCode()
		//m.logger.Printf("EXIT ERROR [%s]", prefix)

		//Stop reading
		stopReadSignalErr <- struct{}{}
		return
	}

	//m.logger.Printf("EXIT SUCCESS [%s]", prefix)
	//Pipe already closed
	stopReadSignal <- struct{}{}

	//Wait stop sending
	wgReaderEnd.Wait()

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
