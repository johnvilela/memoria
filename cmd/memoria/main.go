package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func run(args []string, stdin io.Reader, out io.Writer) int {
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	switch arg {
	case "", "help", "--help", "-h":
		fmt.Fprintln(out, renderHelp())
		return 0
	case "init":
		return runInit(args[1:], defaultConfigPath(), out)
	case "bootstrap":
		fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
		fs.SetOutput(out)
		wiki := fs.String("wiki", "", "wiki folder name (default \"wiki\")")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runBootstrap(cwd, defaultConfigPath(), *wiki, out)
	case "process":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runProcess(cwd, defaultConfigPath(), args[1:], out)
	case "hook":
		// must never block the agent: silent, always 0, nothing on stdout
		if len(args) > 1 {
			if err := captureHook(args[1], stdin, defaultConfigPath()); err != nil {
				logf("hook", "%s: %v", args[1], err)
			}
		}
		return 0
	default:
		fmt.Fprintf(out, "unknown command: %q\n\n", arg)
		fmt.Fprintln(out, renderHelp())
		return 1
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout))
}
