// Copyright 2015 Daniel Theophanes.
// Use of this source code is governed by a zlib-style
// license that can be found in the LICENSE file.

package service

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

var cgroupFile = "/proc/1/cgroup"

type linuxSystemService struct {
	name        string
	detect      func() bool
	interactive func() bool
	new         func(i Interface, platform string, c *Config) (Service, error)
}

func (sc linuxSystemService) String() string {
	return sc.name
}
func (sc linuxSystemService) Detect() bool {
	return sc.detect()
}
func (sc linuxSystemService) Interactive() bool {
	return sc.interactive()
}
func (sc linuxSystemService) New(i Interface, c *Config) (Service, error) {
	return sc.new(i, sc.String(), c)
}

func init() {
	ChooseSystem(linuxSystemService{
		name:        "linux-systemd",
		detect:      isSystemd,
		interactive: isInteractive,
		new:         newSystemdService,
	},
		linuxSystemService{
			name:        "linux-upstart",
			detect:      isUpstart,
			interactive: isInteractive,
			new:         newUpstartService,
		},
		linuxSystemService{
			name:        "linux-openrc",
			detect:      isOpenRC,
			interactive: isInteractive,
			new:         newOpenRCService,
		},
		linuxSystemService{
			name:        "linux-rcs",
			detect:      isRCS,
			interactive: isInteractive,
			new:         newRCSService,
		},
		linuxSystemService{
			name:        "linux-boxrc",
			detect:      isBoxRC,
			interactive: isInteractive,
			new:         newBoxRCService,
		},
		linuxSystemService{
			name:        "linux-systemv",
			detect:      isSysV,
			interactive: isInteractive,
			new:         newSystemVService,
		},
	)
}

func binaryName(pid int) (string, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)

	dataBytes, err := os.ReadFile(statPath)
	if err != nil {
		return "", err
	}

	// First, parse out the image name
	data := string(dataBytes)
	binStart := strings.IndexRune(data, '(') + 1
	binEnd := strings.IndexRune(data[binStart:], ')')
	return data[binStart : binStart+binEnd], nil
}

func isInteractive() bool {
	// we assume we always interactive when containerised
	// if function returns error we cannot determine whether we in container or not so we assume that not
	inContainer, err := isInContainer(cgroupFile)
	if err == nil && inContainer {
		return true
	}

	// parent pid 1 means we started under init system of some sorts
	ppid := os.Getppid()
	if ppid == 1 {
		return false
	}

	binary, _ := binaryName(ppid)
	return binary != "systemd"
}

// isInContainer checks if the service is being executed in docker or lxc
// container.
func isInContainer(cgroupPath string) (bool, error) {
	const maxlines = 5 // maximum lines to scan

	f, err := os.Open(cgroupPath)
	if err != nil {
		return false, err
	}

	defer f.Close()

	scan := bufio.NewScanner(f)

	lines := 0
	for scan.Scan() && !(lines > maxlines) {
		if strings.Contains(scan.Text(), "docker") || strings.Contains(scan.Text(), "lxc") {
			return true, nil
		}
		lines++
	}

	if err := scan.Err(); err != nil {
		return false, err
	}

	return false, nil
}

var tf = map[string]interface{}{
	"cmd": func(s string) string {
		return `"` + strings.Replace(s, `"`, `\"`, -1) + `"`
	},
	"cmdEscape": func(s string) string {
		return strings.Replace(s, " ", `\x20`, -1)
	},
}
