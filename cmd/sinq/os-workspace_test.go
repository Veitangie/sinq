// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOSRootWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	ws, err := NewOSRootWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("NewOSRootWorkspace failed: %v", err)
	}

	if ws.String() != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, ws.String())
	}

	f, err := ws.Create("test.txt")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	f.Close()

	if _, err := os.Stat(filepath.Join(tmpDir, "test.txt")); os.IsNotExist(err) {
		t.Errorf("file was not created")
	}
}

func TestOSWorkspace(t *testing.T) {
	ws := OSWorkspace{}
	if ws.String() != "os" {
		t.Errorf("expected os, got %s", ws.String())
	}

	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	f, err := ws.Create(tmpFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	f.Close()

	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Errorf("file was not created")
	}
}
