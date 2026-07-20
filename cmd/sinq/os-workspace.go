// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"io"
	"io/fs"
	"os"

	"github.com/Veitangie/sinq/internal/runner"
)

const PERM_RWX fs.FileMode = 0777
const PERM_RW fs.FileMode = 0666
const O_CRWRTR int = os.O_CREATE | os.O_WRONLY | os.O_TRUNC

type OSRootWorkspace struct {
	root       *os.Root
	rootString string
}

func NewOSRootWorkspace(dir string) (OSRootWorkspace, error) {
	root, err := os.OpenRoot(dir)
	return OSRootWorkspace{root, dir}, err
}

var _ runner.Workspace = OSRootWorkspace{}

func (ws OSRootWorkspace) Stat(filename string) (fs.FileInfo, error) {
	return ws.root.Stat(filename)
}

func (ws OSRootWorkspace) Open(filename string) (fs.File, error) {
	return ws.root.Open(filename)
}

func (ws OSRootWorkspace) Create(filename string) (io.WriteCloser, error) {
	return ws.root.OpenFile(filename, O_CRWRTR, PERM_RW)
}

func (ws OSRootWorkspace) String() string {
	return ws.rootString
}

type OSWorkspace struct{}

var _ runner.Workspace = OSWorkspace{}

func (ws OSWorkspace) Stat(filename string) (fs.FileInfo, error) {
	return os.Stat(filename)
}

func (ws OSWorkspace) Open(filename string) (fs.File, error) {
	return os.Open(filename)
}

func (ws OSWorkspace) Create(filename string) (io.WriteCloser, error) {
	return os.OpenFile(filename, O_CRWRTR, PERM_RW)
}

func (ws OSWorkspace) String() string {
	return "os"
}
