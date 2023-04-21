### Extends basic formaters(`go mod tidy` and `goimports`) that do not merge groups before formatting. This causes to a lot of conflicts when the same modules are distributed in different groups

Install
```shell
go install github.com/r-usenko/godeFmt/cmd/...@latest
```

Example for sort go.mod and imports with repo and apply changes to files
```shell
godeFmt -tidy -imports-prefix=github.com/r-usenko -write -dir=.
```
