package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mattn/go-shellwords"
	"github.com/urfave/cli/v2"

	"github.com/acoshift/goreload/internal"
)

var (
	logger      = log.New(os.Stdout, "[goreload] ", 0)
	colorGreen  = string([]byte{27, 91, 57, 55, 59, 51, 50, 59, 49, 109})
	colorYellow = string([]byte{27, 91, 57, 55, 59, 51, 51, 59, 49, 109})
	colorRed    = string([]byte{27, 91, 57, 55, 59, 51, 49, 59, 49, 109})
	colorReset  = string([]byte{27, 91, 48, 109})
)

func main() {
	app := cli.NewApp()
	app.Name = "goreload"
	app.Usage = "A live reload utility for Go web applications."
	app.Action = mainAction
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "bin",
			Aliases: []string{"b"},
			Value:   ".goreload",
			Usage:   "name of generated binary file",
		},
		&cli.StringFlag{
			Name:    "path",
			Aliases: []string{"t"},
			Value:   ".",
			Usage:   "Path to watch files from",
		},
		&cli.StringFlag{
			Name:    "build",
			Aliases: []string{"d"},
			Value:   "",
			Usage:   "Path to build files from (defaults to same value as --path)",
		},
		&cli.StringSliceFlag{
			Name:    "excludeDir",
			Aliases: []string{"x"},
			Value:   &cli.StringSlice{},
			Usage:   "Relative directories to exclude",
		},
		&cli.BoolFlag{
			Name:  "all",
			Usage: "reloads whenever any file changes, as opposed to reloading only on .go file change",
		},
		&cli.StringFlag{
			Name:  "buildArgs",
			Usage: "Additional go build arguments",
		},
		&cli.StringFlag{
			Name:  "logPrefix",
			Usage: "Log prefix",
			Value: "goreload",
		},
	}
	app.Commands = []*cli.Command{
		{
			Name:    "run",
			Aliases: []string{"r"},
			Usage:   "Run the goreload",
			Action:  mainAction,
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err)
	}
}

func mainAction(c *cli.Context) error {
	logger.SetPrefix(fmt.Sprintf("[%s] ", c.String("logPrefix")))

	all := c.Bool("all")
	wd, err := os.Getwd()
	if err != nil {
		logger.Fatal(err)
		return err
	}

	buildArgs, err := shellwords.Parse(c.String("buildArgs"))
	if err != nil {
		return err
	}

	buildPath := c.String("build")
	if buildPath == "" {
		buildPath = c.String("path")
	}
	builder := internal.NewBuilder(buildPath, c.String("bin"), wd, buildArgs)
	runner := internal.NewRunner(filepath.Join(wd, builder.Binary()), c.Args().Slice()...)
	runner.SetWriter(os.Stdout)

	shutdown(runner)

	ctx, cancel := context.WithCancel(context.Background())
	buildAndRun(ctx, builder, runner)
	scanChanges(c.String("path"), c.StringSlice("excludeDir"), all, func() {
		cancel()
		runner.Kill()
		ctx, cancel = context.WithCancel(context.Background())
		go buildAndRun(ctx, builder, runner)
	})

	return nil
}

var lockBuildAndRun sync.Mutex

func buildAndRun(ctx context.Context, builder *internal.Builder, runner *internal.Runner) {
	// allow only single buildAndRun at anytime
	lockBuildAndRun.Lock()
	defer lockBuildAndRun.Unlock()

	logger.Println("Building...")

	err := builder.Build(ctx)
	if err == context.Canceled {
		logger.Printf("%sBuild canceled%s\n", colorYellow, colorReset)
		return
	}
	if err != nil {
		logger.Printf("%sBuild failed%s\n", colorRed, colorReset)
		fmt.Println(err)
		return
	}

	logger.Printf("%sBuild finished%s\n", colorGreen, colorReset)
	runner.Run()
}

func scanChanges(watchPath string, excludeDirs []string, allFiles bool, cb func()) {
	scanChangesFswatch(watchPath, excludeDirs, allFiles, cb)
	scanChangesWalk(watchPath, excludeDirs, allFiles, cb)
}

func scanChangesFswatch(watchPath string, excludeDirs []string, allFiles bool, cb func()) {
	curDir, err := os.Getwd()
	if err != nil {
		return
	}
	curDir += "/"
	debouncedCallback := newDebounce(cb, 100*time.Millisecond)

	// always retry when fswatch exit
	for {
		func() {
			cmd := exec.Command("fswatch",
				"-r",
				"--event=Created",
				"--event=Updated",
				"--event=Removed",
				watchPath,
			)
			p, err := cmd.StdoutPipe()
			if err != nil {
				return
			}
			err = cmd.Start()
			if err != nil {
				// fswatch not found, or can not start
				return
			}
			defer func() {
				if cmd.Process != nil {
					cmd.Process.Kill()
				}
			}()

			r := bufio.NewReader(p)
			for {
				pathBytes, _, err := r.ReadLine()
				if err != nil {
					break
				}
				path := string(pathBytes)
				path = strings.TrimPrefix(path, curDir)

				if strings.HasPrefix(path, ".git/") {
					continue
				}
				{
					skip := false
					for _, x := range excludeDirs {
						if strings.HasPrefix(path, x) {
							skip = true
							break
						}
					}
					if skip {
						continue
					}
				}
				if filepath.Base(path)[0] == '.' {
					continue
				}

				if !(allFiles || filepath.Ext(path) == ".go") {
					continue
				}

				debouncedCallback.Call()
			}
		}()
		time.Sleep(500 * time.Millisecond)
	}
}

func scanChangesWalk(watchPath string, excludeDirs []string, allFiles bool, cb func()) {
	excludeDir := make(map[string]bool)
	for _, x := range excludeDirs {
		excludeDir[x] = true
	}

	startTime := time.Now()
	var errDone = errors.New("done")
	for {
		filepath.Walk(watchPath, func(path string, info os.FileInfo, err error) error {
			if path == ".git" && info.IsDir() {
				return filepath.SkipDir
			}
			if excludeDir[path] {
				return filepath.SkipDir
			}

			// ignore hidden files
			if filepath.Base(path)[0] == '.' {
				return nil
			}

			if (allFiles || filepath.Ext(path) == ".go") && info.ModTime().After(startTime) {
				cb()
				startTime = time.Now()
				return errDone
			}

			return nil
		})
		time.Sleep(500 * time.Millisecond)
	}
}

func shutdown(runner *internal.Runner) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-c
		log.Println("Got signal: ", s)
		err := runner.Kill()
		if err != nil {
			log.Print("Error killing: ", err)
		}
		os.Exit(1)
	}()
}

type debounce struct {
	mu sync.Mutex
	t  *time.Timer
	f  func()
	d  time.Duration
}

func newDebounce(f func(), d time.Duration) *debounce {
	return &debounce{
		f: f,
		d: d,
	}
}

func (d *debounce) Call() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.t == nil {
		d.f()
		d.t = time.AfterFunc(0, func() {})
		return
	}

	d.t.Stop()
	d.t = time.AfterFunc(d.d, d.f)
}
