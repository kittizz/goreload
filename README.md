# Goreload

`goreload` forks from acoshift/goreload and add features kill signal.

Just run `goreload` in your app directory.
`goreload` will automatically recompile your code when it
detects a change.

## Installation

```shell
go install github.com/kittizz/goreload@master
```

### macOS

**Optional** Use [fswatch](https://github.com/emcrisostomo/fswatch)

```shell
brew install fswatch
```

## Basic usage

```shell
goreload main.go
```

Options

```txt
   --bin value, -b value         name of generated binary file (default: ".goreload")
   --path value, -t value        Path to watch files from (default: ".")
   --build value, -d value       Path to build files from (defaults to same value as --path)
   --excludeDir value, -x value  Relative directories to exclude
   --all                         reloads whenever any file changes, as opposed to reloading only on .go file change
   --buildArgs value             Additional go build arguments
   --logPrefix value             Setup custom log prefix
   --help, -h                    show help
   --version, -v                 print the version
   --signal, -s                  kill by signal code (defaults: "SIGKILL" | "Interrupt", "SIGTERM", "SIGINT", "SIGHUP", "SIGQUIT")
```
