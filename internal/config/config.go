// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"log/slog"
	"regexp"
)

type Config struct {
	Workers       int
	Safe          bool
	Insecure      bool
	Version       bool
	Help          bool
	List          bool
	DumpOnFailure bool

	LogLevel   slog.Level
	Format     string
	Out        string
	Paths      []string
	Treewalker TreewalkerConfig
	Reporter   ReporterConfig

	TagsInclude  []string
	NamesInclude []regexp.Regexp
	TagsExclude  []string
	NamesExclude []regexp.Regexp
}

func (c Config) ShouldInclude(tags map[string]bool, name string) bool {
	for _, tag := range c.TagsExclude {
		if _, found := tags[tag]; found {
			return false
		}
	}
	for _, regex := range c.NamesExclude {
		if match := regex.FindStringIndex(name); len(match) != 0 {
			return false
		}
	}
	for _, tag := range c.TagsInclude {
		if _, found := tags[tag]; found {
			return true
		}
	}
	for _, regex := range c.NamesInclude {
		if match := regex.FindStringIndex(name); len(match) != 0 {
			return true
		}
	}
	return len(c.TagsInclude) == 0 && len(c.NamesInclude) == 0
}

type TreewalkerConfig struct {
	Strict      bool
	SecretsFile string
	Secret      map[string]string
	Env         map[string]string
}

type WhenColor int

const (
	Never WhenColor = iota
	Always
	Auto
)

type WhatShow int

const (
	All WhatShow = iota
	NoSkip
	Failures
)

type ReporterConfig struct {
	Verbose bool
	Color   WhenColor
	Show    WhatShow
}

func SaneDefaults() Config {
	return Config{
		Workers:       10,
		Safe:          false,
		Insecure:      false,
		Version:       false,
		Help:          false,
		List:          false,
		DumpOnFailure: false,
		LogLevel:      slog.LevelWarn,
		Format:        "std",
		Out:           "",
		Paths:         []string{},
		Treewalker: TreewalkerConfig{
			Strict:      true,
			SecretsFile: "",
			Secret:      map[string]string{},
			Env:         map[string]string{},
		},
		Reporter: ReporterConfig{
			Verbose: false,
			Color:   Auto,
			Show:    NoSkip,
		},
	}
}
