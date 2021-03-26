package internal

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Builder struct {
	dir       string
	binary    string
	wd        string
	buildArgs []string
}

// NewBuilder creates new builder
func NewBuilder(dir string, bin string, wd string, buildArgs []string) *Builder {
	if len(bin) == 0 {
		bin = "bin"
	}

	// does not work on Windows without the ".exe" extension
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(bin, ".exe") { // check if it already has the .exe extension
			bin += ".exe"
		}
	}

	if dir == "" {
		dir = "."
	} else {
		dir = filepath.Join(wd, dir)
	}

	return &Builder{dir: dir, binary: bin, wd: wd, buildArgs: buildArgs}
}

func (b *Builder) Binary() string {
	return b.binary
}

func (b *Builder) Build(ctx context.Context) error {
	args := append([]string{"go", "build", "-o", filepath.Join(b.wd, b.binary)}, b.buildArgs...)
	args = append(args, b.dir)

	var buf bytes.Buffer
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	result := make(chan error, 1)
	go func() {
		err := cmd.Run()
		if err != nil {
			err = fmt.Errorf(string(buf.Bytes()) + err.Error())
		}
		result <- err
	}()

	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		return ctx.Err()
	case err := <-result:
		return err
	}
}
