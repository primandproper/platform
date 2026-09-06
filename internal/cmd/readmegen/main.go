package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func main() {
	root := flag.String("root", "", "module root to read; defaults to the nearest directory at or above the working directory holding go.mod")
	out := flag.String("out", "README.md", "file to rewrite, relative to the module root")

	flag.Parse()

	if err := run(*root, *out); err != nil {
		fmt.Fprintln(os.Stderr, "readmegen:", err)
		os.Exit(1)
	}
}

func run(root, out string) error {
	if root == "" {
		found, err := moduleRoot()
		if err != nil {
			return err
		}

		root = found
	}

	surveyed, err := surveyTree(root)
	if err != nil {
		return err
	}

	changed, err := rewrite(filepath.Join(root, filepath.FromSlash(out)), surveyed.regions())
	if err != nil {
		return err
	}

	if changed {
		fmt.Fprintf(os.Stderr, "readmegen: rewrote %s from %d transports and %d stores\n", out, len(surveyed.transports), len(surveyed.stores))
	}

	return nil
}

// moduleRoot is the nearest directory at or above the working directory holding
// a go.mod, which is what lets `go generate` run this from the command's own
// directory and still have it describe the module rather than that directory.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no go.mod at or above the working directory")
		}

		dir = parent
	}
}
