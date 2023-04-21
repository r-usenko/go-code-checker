```shell
go install github.com/r-usenko/godeFmt/cmd/...@latest
```

Example for sort go.mod and imports with repo and apply changes to files

```shell
godeFmt -tidy -write -imports-prefix=github.com/r-usenko 
```
