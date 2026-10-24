// Package cli is the duls command surface: the command and the router.
// It exists so the process body and the black-box process scenarios route
// through exactly the same command.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/baalimago/duls/cmd/duls"
	"github.com/baalimago/duls/internal/app"
)

// command is the router's view of a command: its help and the two calls
// the router makes.
type command interface {
	Help() string
	Parse([]string) error
	Run(context.Context) error
}

// Run routes args to the command and returns the process exit code.
func Run(ctx context.Context, args []string, opts app.Options) int {
	opts = withProcessEnv(opts)
	return router{command: duls.Command(opts), opts: opts}.run(ctx, args)
}

// withProcessEnv resolves a nil Env to the process environment and stores
// it in the options, so the command and the colour gate behind it see the
// same environment.
func withProcessEnv(opts app.Options) app.Options {
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	return opts
}

type router struct {
	command command
	opts    app.Options
}

// run parses the command line, then calls the command's Setup and Run, and
// returns the exit code: 0 on success and for help, 1 for everything else.
func (r router) run(ctx context.Context, args []string) int {
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}
	if err := r.command.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.help()
			return 0
		}
		r.fail(err)
		return 1
	}
	if err := r.command.Run(ctx); err != nil {
		r.fail(err)
		return 1
	}
	return 0
}

// help prints the command's help on stdout.
func (r router) help() {
	io.WriteString(r.opts.Out(), r.command.Help())
}

// fail prints one error line on stderr, so scripts can read it whole.
func (r router) fail(err error) {
	fmt.Fprintf(r.opts.ErrOut(), "error: %s\n", strings.ReplaceAll(err.Error(), "\n", " "))
}
