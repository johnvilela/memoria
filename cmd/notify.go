package main

import (
	"os/exec"
	"runtime"
)

// notifyCmd sends one desktop notification. Var so tests can stub it.
// ponytail: notify-send only — osascript for macOS when a mac user asks
var notifyCmd = func(title, body string) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	return exec.Command("notify-send", "--app-name=memoria", title, body).Run()
}

// notify pings the desktop when the user opted in (memoria init
// --notification). Failures only hit the log — never the run.
func notify(cfg config, title, body string) {
	if !cfg.Notifications {
		return
	}
	if err := notifyCmd(title, body); err != nil {
		logf("notify", "%v", err)
	}
}
