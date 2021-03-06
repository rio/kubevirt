/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2021 Red Hat, Inc.
 *
 */

package selinux

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/opencontainers/selinux/go-selinux"
)

const (
	minFDToCloseOnExec = 3
	maxFDToCloseOnExec = 256
)

type ContextExecutor struct {
	cmdToExecute  *exec.Cmd
	desiredLabel  string
	originalLabel string
	pid           int
}

func NewContextExecutor(pid int, cmd *exec.Cmd) (*ContextExecutor, error) {
	desiredLabel, err := getLabelForPID(pid)
	if err != nil {
		return nil, err
	}
	originalLabel, err := getLabelForPID(os.Getpid())
	if err != nil {
		return nil, err
	}
	return &ContextExecutor{
		pid:           pid,
		cmdToExecute:  cmd,
		desiredLabel:  desiredLabel,
		originalLabel: originalLabel,
	}, nil
}

func (ce ContextExecutor) Execute() error {
	if isSELinuxEnabled() {
		if err := ce.setDesiredContext(); err != nil {
			return err
		}
		defer ce.resetContext()
	}

	preventFDLeakOntoChild()
	if err := ce.cmdToExecute.Run(); err != nil {
		return fmt.Errorf("failed to execute command in launcher namespace %d: %v", ce.pid, err)
	}
	return nil
}

func (ce ContextExecutor) setDesiredContext() error {
	runtime.LockOSThread()
	if err := selinux.SetExecLabel(ce.desiredLabel); err != nil {
		return fmt.Errorf("failed to switch selinux context to %s. Reason: %v", ce.desiredLabel, err)
	}
	return nil
}

func (ce ContextExecutor) resetContext() error {
	defer runtime.UnlockOSThread()
	return selinux.SetExecLabel(ce.originalLabel)
}

func isSELinuxEnabled() bool {
	_, selinuxEnabled, err := NewSELinux()
	return err == nil && selinuxEnabled
}

func getLabelForPID(pid int) (string, error) {
	fileLabel, err := selinux.FileLabel(fmt.Sprintf("/proc/%d/attr/current", pid))
	if err != nil {
		return "", fmt.Errorf("could not retrieve pid %d selinux label: %v", pid, err)
	}
	return fileLabel, nil
}

func preventFDLeakOntoChild() {
	// we want to share the parent process std{in|out|err} - fds 0 through 2.
	// Since the FDs are inherited on fork / exec, we close on exec all others.
	for fd := minFDToCloseOnExec; fd < maxFDToCloseOnExec; fd++ {
		syscall.CloseOnExec(fd)
	}
}
