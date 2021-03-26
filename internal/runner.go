package internal

import (
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

type Runner struct {
	lock      sync.Mutex
	bin       string
	args      []string
	writer    io.Writer
	command   *exec.Cmd
	startTime time.Time
}

// NewRunner creates new runner
func NewRunner(bin string, args ...string) *Runner {
	return &Runner{
		bin:       bin,
		args:      args,
		writer:    ioutil.Discard,
		startTime: time.Now(),
	}
}

func (r *Runner) Run() (*exec.Cmd, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.needsRefresh() {
		r.killLocked()
	}

	if r.command == nil || r.exited() {
		err := r.runBin()
		if err != nil {
			log.Print("Error running: ", err)
		}
		time.Sleep(250 * time.Millisecond)
		return r.command, err
	}
	return r.command, nil
}

func (r *Runner) Info() (os.FileInfo, error) {
	return os.Stat(r.bin)
}

func (r *Runner) SetWriter(writer io.Writer) {
	r.writer = writer
}

func (r *Runner) Kill() error {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.killLocked()
}

func (r *Runner) killLocked() error {
	if r.command == nil {
		return nil
	}
	if r.command.Process == nil {
		return nil
	}

	done := make(chan error)
	go func() {
		r.command.Wait()
		close(done)
	}()

	// trying a "soft" kill first
	if runtime.GOOS == "windows" {
		err := r.command.Process.Kill()
		if err != nil {
			return err
		}
	} else {
		err := r.command.Process.Signal(os.Interrupt)
		if err != nil {
			return err
		}
	}

	// wait for our process to die before we return or hard kill after 3 sec
	select {
	case <-time.After(3 * time.Second):
		if err := r.command.Process.Kill(); err != nil {
			log.Println("failed to kill: ", err)
		}
	case <-done:
	}
	r.command = nil

	return nil
}

func (r *Runner) exited() bool {
	return r.command != nil && r.command.ProcessState != nil && r.command.ProcessState.Exited()
}

func (r *Runner) runBin() error {
	r.command = exec.Command(r.bin, r.args...)
	stdout, err := r.command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := r.command.StderrPipe()
	if err != nil {
		return err
	}

	err = r.command.Start()
	if err != nil {
		return err
	}

	r.startTime = time.Now()

	go io.Copy(r.writer, stdout)
	go io.Copy(r.writer, stderr)
	go r.command.Wait()

	return nil
}

func (r *Runner) needsRefresh() bool {
	info, err := r.Info()
	if err != nil {
		return false
	}
	return info.ModTime().After(r.startTime)
}
