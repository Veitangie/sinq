package main

import (
	"io"
	"io/fs"
	"os"

	"github.com/Veitangie/sinq/internal/runner"
)

const PERM_RW fs.FileMode = 0666
const O_CRWRTR int = os.O_CREATE | os.O_WRONLY | os.O_TRUNC

type OSWorkspace struct {
	root *os.Root
}

func NewOSWorkspace(dir string) (OSWorkspace, error) {
	root, err := os.OpenRoot(dir)
	return OSWorkspace{root}, err
}

var _ runner.Workspace = OSWorkspace{}

func (ws OSWorkspace) Stat(filename string) (fs.FileInfo, error) {
	return ws.root.Stat(filename)
}

func (ws OSWorkspace) Open(filename string) (fs.File, error) {
	return ws.root.Open(filename)
}

func (ws OSWorkspace) Create(filename string) (io.WriteCloser, error) {
	return ws.root.OpenFile(filename, O_CRWRTR, PERM_RW)
}
