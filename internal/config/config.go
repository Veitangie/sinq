// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

type Config struct {
	Workers  int
	Verbose  bool
	Safe     bool
	Insecure bool
	Version  bool
	Help     bool
	List     bool

	Format     string
	Secrets    string
	Out        string
	Paths      []string
	Treewalker TreewalkerConfig
	Reporter   ReporterConfig
}

type TreewalkerConfig struct {
	Strict bool
}

type WhenColor int

const (
	Never WhenColor = iota
	Always
	Auto
)

type ReporterConfig struct {
	Verbose bool
	Color   WhenColor
}

func SaneDefaults() Config {
	return Config{
		Workers:  10,
		Verbose:  false,
		Safe:     false,
		Insecure: false,
		Version:  false,
		Help:     false,
		List:     false,
		Format:   "default",
		Secrets:  "",
		Out:      "",
		Paths:    []string{},
		Treewalker: TreewalkerConfig{
			Strict: true,
		},
		Reporter: ReporterConfig{
			Verbose: false,
			Color:   Auto,
		},
	}
}
