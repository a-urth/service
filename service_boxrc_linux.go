// Copyright 2015 Daniel Theophanes.
// Use of this source code is governed by a zlib-style
// license that can be found in the LICENSE file.

package service

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"text/template"
	"time"
)

type boxrc struct {
	i        Interface
	platform string
	*Config
}

func isBoxRC() bool {
	if _, err := os.Stat("/etc/boxinit.d"); err != nil {
		return false
	}

	if _, err := os.Stat("/etc/inittab"); err == nil {
		filerc, err := os.Open("/etc/inittab")
		if err != nil {
			return false
		}
		defer filerc.Close()

		buf := new(bytes.Buffer)
		buf.ReadFrom(filerc)
		contents := buf.String()

		re := regexp.MustCompile(`::sysinit:.*boxrc\.d`)
		matches := re.FindStringSubmatch(contents)
		if len(matches) > 0 {
			return true
		}
		return false
	}

	return false
}

func newBoxRCService(i Interface, platform string, c *Config) (Service, error) {
	s := &boxrc{
		i:        i,
		platform: platform,
		Config:   c,
	}

	if s.Option.bool(optionUserService, optionUserServiceDefault) {
		return nil, errNoUserServiceRCS
	}

	return s, nil
}

func (s *boxrc) String() string {
	if len(s.DisplayName) > 0 {
		return s.DisplayName
	}
	return s.Name
}

func (s *boxrc) Platform() string {
	return s.platform
}

func (s *boxrc) configPath() string {
	return "/etc/boxinit.d/65" + s.Config.Name
}

func (s *boxrc) template() *template.Template {
	customScript := s.Option.string(optionRCSScript, "")

	if customScript != "" {
		return template.Must(template.New("").Funcs(tf).Parse(customScript))
	}
	return template.Must(template.New("").Funcs(tf).Parse(boxrcScript))
}

func (s *boxrc) Install() error {
	confPath := s.configPath()

	if _, err := os.Stat(confPath); err == nil {
		return fmt.Errorf("Init already exists: %s", confPath)
	}

	f, err := os.Create(confPath)
	if err != nil {
		return err
	}

	defer f.Close()

	path, err := s.execPath()
	if err != nil {
		return err
	}

	var to = &struct {
		*Config
		Path         string
		LogDirectory string
	}{
		s.Config,
		path,
		s.Option.string(optionLogDirectory, defaultLogDirectory),
	}

	err = s.template().Execute(f, to)
	if err != nil {
		return err
	}

	if err = os.Chmod(confPath, 0755); err != nil {
		return err
	}

	if err = os.Symlink(confPath, "/etc/boxrc.d/65"+s.Name); err != nil {
		return err
	}

	return nil
}

func (s *boxrc) Uninstall() error {
	if err := os.Remove(s.configPath()); err != nil {
		return err
	}

	if err := os.Remove("/etc/boxrc.d/65" + s.Name); err != nil {
		return err
	}

	return nil
}

func (s *boxrc) Logger(errs chan<- error) (Logger, error) {
	if system.Interactive() {
		return ConsoleLogger, nil
	}
	return s.SystemLogger(errs)
}
func (s *boxrc) SystemLogger(errs chan<- error) (Logger, error) {
	return newSysLogger(s.Name, errs)
}

func (s *boxrc) Run() (err error) {
	err = s.i.Start(s)
	if err != nil {
		return err
	}

	s.Option.funcSingle(optionRunWait, func() {
		var sigChan = make(chan os.Signal, 3)
		signal.Notify(sigChan, syscall.SIGTERM, os.Interrupt)
		<-sigChan
	})()

	return s.i.Stop(s)
}

func (s *boxrc) Status() (Status, error) {
	_, out, err := runWithOutput(s.configPath(), "status")
	if err != nil {
		return StatusUnknown, err
	}

	switch {
	case strings.HasPrefix(out, "Running"):
		return StatusRunning, nil
	case strings.HasPrefix(out, "Stopped"):
		return StatusStopped, nil
	default:
		return StatusUnknown, ErrNotInstalled
	}
}

func (s *boxrc) Start() error {
	return run(s.configPath(), "start")
}

func (s *boxrc) Stop() error {
	return run(s.configPath(), "stop")
}

func (s *boxrc) Restart() error {
	err := s.Stop()
	if err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return s.Start()
}

const boxrcScript = `#!/bin/sh

cmd="{{.Path}}{{range .Arguments}} {{.|cmd}}{{end}}"

name={{.Name}}
pid_file="/var/run/$name.pid"
stdout_log="{{.LogDirectory}}/$name.log"
stderr_log="{{.LogDirectory}}/$name.err"

get_pid() {
    cat "$pid_file"
}

is_running() {
    [ -f "$pid_file" ] && cat /proc/$(get_pid)/stat > /dev/null 2>&1
}

case "$1" in
    start)
        if is_running; then
            echo "Already started"
        else
            echo "Starting $name"
            {{if .WorkingDirectory}}cd '{{.WorkingDirectory}}'{{end}}
            $cmd >> "$stdout_log" 2>> "$stderr_log" &
            echo $! > "$pid_file"
            if ! is_running; then
                echo "Unable to start, see $stdout_log and $stderr_log"
                exit 1
            fi
        fi
    ;;
    stop)
        if is_running; then
            echo -n "Stopping $name.."
            kill $(get_pid)
            for i in $(seq 1 10)
            do
                if ! is_running; then
                    break
                fi
                echo -n "."
                sleep 1
            done
            echo
            if is_running; then
                echo "Not stopped; may still be shutting down or shutdown may have failed"
                exit 1
            else
                echo "Stopped"
                if [ -f "$pid_file" ]; then
                    rm "$pid_file"
                fi
            fi
        else
            echo "Not running"
        fi
    ;;
    restart)
        $0 stop
        if is_running; then
            echo "Unable to stop, will not attempt to start"
            exit 1
        fi
        $0 start
    ;;
    status)
        if is_running; then
            echo "Running"
        else
            echo "Stopped"
            exit 0
        fi
    ;;
    *)
    echo "Usage: $0 {start|stop|restart|status}"
    exit 1
    ;;
esac
`
