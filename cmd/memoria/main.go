package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// version is bumped by hand on release, together with the matching git tag.
const version = "0.10.2"

func run(args []string, stdin io.Reader, out io.Writer) int {
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	switch arg {
	case "", "help", "--help", "-h":
		fmt.Fprintln(out, renderHelp())
		return 0
	case "version", "--version", "-v":
		fmt.Fprintln(out, "memoria "+version)
		return 0
	case "init":
		return runInit(args[1:], defaultConfigPath(), out)
	case "setup":
		return runSetup(args[1:], defaultConfigPath(), out)
	case "bootstrap":
		fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
		fs.SetOutput(out)
		wiki := fs.String("wiki", "", "wiki folder name (default \"wiki\")")
		background := fs.Bool("background", false, "seed the wiki from git history in a detached background run")
		seedFg := fs.Bool("seed-foreground", false, "internal: run the seed inline (spawned by --background)")
		global := fs.Bool("global", false, "capture sessions in every folder, not just registered projects")
		gpath := fs.String("global-path", "", "global capture root (default: the memoria config folder); requires --global")
		if err := fs.Parse(args[1:]); err != nil {
			return 1
		}
		if *gpath != "" && !*global {
			fmt.Fprintln(out, "error: --global-path requires --global")
			return 1
		}
		if *global {
			if *wiki != "" || *background {
				fmt.Fprintln(out, "error: --wiki and --background cannot be combined with --global")
				return 1
			}
			return runBootstrapGlobal(defaultConfigPath(), *gpath, out)
		}
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runBootstrap(cwd, defaultConfigPath(), *wiki, *background, *seedFg, out)
	case "process":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runProcess(cwd, defaultConfigPath(), args[1:], out)
	case "lint":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runLint(cwd, defaultConfigPath(), args[1:], out)
	case "run":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runRun(cwd, defaultConfigPath(), args[1:], out)
	case "search":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runSearch(cwd, defaultConfigPath(), args[1:], out)
	case "commit":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runCommit(cwd, defaultConfigPath(), args[1:], out)
	case "digest":
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		return runDigest(cwd, defaultConfigPath(), args[1:], out)
	case "status":
		return runStatus(defaultConfigPath(), out)
	case "update":
		return runUpdate(args[1:], out)
	case "mcp":
		return runMCP(defaultConfigPath(), out)
	case "hook":
		// must never block the agent: silent, always 0, nothing on stdout
		if len(args) > 1 {
			if err := captureHook(args[1], args[2:], stdin, defaultConfigPath()); err != nil {
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
