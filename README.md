```shell
go install github.com/r-usenko/go-code-checker/cmd/...@latest
```

Example for sort go.mod and imports with repo and apply changes to files

```shell
gocodechecker -tidy -write -imports-prefix=github.com/r-usenko 
```
