// Copyright 2015 Daniel Theophanes.
// Use of this source code is governed by a zlib-style
// license that can be found in the LICENSE file.

//go:build linux || darwin || solaris || aix || freebsd
// +build linux darwin solaris aix freebsd

package service

import (
	"bytes"
	"fmt"
	"log/syslog"
	"os/exec"
	"strings"
	"syscall"
)

const defaultLogDirectory = "/var/log"

func newSysLogger(name string, errs chan<- error) (Logger, error) {
	w, err := syslog.New(syslog.LOG_INFO, name)
	if err != nil {
		return nil, err
	}
	return sysLogger{w, errs}, nil
}

type sysLogger struct {
	*syslog.Writer
	errs chan<- error
}

func (s sysLogger) send(err error) error {
	if err != nil && s.errs != nil {
		s.errs <- err
	}
	return err
}

func (s sysLogger) Error(v ...interface{}) error {
	return s.send(s.Writer.Err(fmt.Sprint(v...)))
}
func (s sysLogger) Warning(v ...interface{}) error {
	return s.send(s.Writer.Warning(fmt.Sprint(v...)))
}
func (s sysLogger) Info(v ...interface{}) error {
	return s.send(s.Writer.Info(fmt.Sprint(v...)))
}
func (s sysLogger) Errorf(format string, a ...interface{}) error {
	return s.send(s.Writer.Err(fmt.Sprintf(format, a...)))
}
func (s sysLogger) Warningf(format string, a ...interface{}) error {
	return s.send(s.Writer.Warning(fmt.Sprintf(format, a...)))
}
func (s sysLogger) Infof(format string, a ...interface{}) error {
	return s.send(s.Writer.Info(fmt.Sprintf(format, a...)))
}

func run(command string, arguments ...string) error {
	_, _, err := runCommand(command, false, arguments...)
	return err
}

func runWithOutput(command string, arguments ...string) (int, string, error) {
	return runCommand(command, true, arguments...)
}

func runCommand(command string, readStdout bool, arguments ...string) (int, string, error) {
	cmd := exec.Command(command, arguments...)

	out, errOut := bytes.NewBuffer(nil), bytes.NewBuffer(nil)

	if readStdout {
		cmd.Stdout = out
	}

	cmd.Stderr = errOut

	// Do not use cmd.Run()
	if err := cmd.Start(); err != nil {
		// Problem while copying stdin, stdout, or stderr
		return 0, "", fmt.Errorf("%q failed: %v", command, err)
	}

	if err := cmd.Wait(); err != nil {
		exitStatus, ok := isExitError(err)
		if ok {
			// Command didn't exit with a zero exit status.
			return exitStatus, out.String(), err
		}

		// An error occurred and there is no exit status.
		return 0, out.String(), fmt.Errorf("%q failed: %v", command, err)
	}

	// Zero exit status
	// Darwin: launchctl can fail with a zero exit status,
	// so check for emtpy stderr
	if command == "launchctl" {
		if errOut.Len() > 0 && !strings.HasSuffix(errOut.String(), "Operation now in progress\n") {
			return 0, "", fmt.Errorf("%q failed with stderr: %s", command, errOut.String())
		}
	}

	return 0, out.String(), nil
}

func isExitError(err error) (int, bool) {
	if exiterr, ok := err.(*exec.ExitError); ok {
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus(), true
		}
	}

	return 0, false
}
