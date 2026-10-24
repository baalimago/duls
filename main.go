// Command duls prints the size view of a directory: the view slivingdoc
// prints when an initial pull materializes a notebook into an empty
// directory. It counts files and their lines, then renders the same
// outline: a strip of the top-level entries by file count, the file and
// line totals, and one row per entry with its files, share, and largest
// children.
//
// Stdout carries the view; errors go to stderr as one line.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/baalimago/duls/internal/app"
	"github.com/baalimago/duls/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args, app.Options{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}))
}
