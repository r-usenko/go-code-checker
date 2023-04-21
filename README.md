### Extends basic formaters(`go mod tidy` and `goimports`) that do not merge groups before formatting. This causes to a lot of conflicts when the same modules are distributed in different groups

Install
```shell
GOPROXY=direct go install -ldflags="-X 'github.com/r-usenko/godeFmt.Version=`git fetch && git describe --tags`'" github.com/r-usenko/godeFmt/cmd/...@latest
```

Example for sort *go.mod* and *imports* with repo prefix and apply changes to files.

```shell
godeFmt -tidy -imports-prefix=github.com/r-usenko -write -dir=.
```

Unfortunately, due to the specificity, it is not possible to process the files in all cases without making changes to the files themselves, so that it can be used as a check. Although most of the formatter errors can be catched and rollback to the original state of the files. 
Do not forget about this if you do not use the `-write` flag
