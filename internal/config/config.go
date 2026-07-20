// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Workers       int
	Insecure      bool
	Version       bool
	Help          bool
	List          bool
	DumpOnFailure bool
	Unrestricted  bool

	LogLevel     slog.Level
	Format       string
	Out          string
	MaxCacheSize DataSize
	CacheTimeout time.Duration
	LuaPaths     []string
	Paths        []string
	Treewalker   TreewalkerConfig
	Reporter     ReporterConfig

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
	isInTags := len(c.TagsInclude) == 0
	for _, tag := range c.TagsInclude {
		if _, found := tags[tag]; found {
			isInTags = true
		}
	}
	for _, regex := range c.NamesInclude {
		if match := regex.FindStringIndex(name); len(match) != 0 {
			return isInTags
		}
	}
	return isInTags && len(c.NamesInclude) == 0
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

type DataSize struct {
	ByteAmount uint64
	Unit       DataUnit
}

func (d DataSize) String() string {
	unitAmount := float64(d.ByteAmount) / float64(d.Unit)
	return fmt.Sprintf("%f%v", unitAmount, d.Unit)
}

type DataUnit int

const (
	Byte   DataUnit = 1
	KiByte DataUnit = 1 << 10
	MiByte DataUnit = 1 << 20
	GiByte DataUnit = 1 << 30
)

func (d DataUnit) String() string {
	switch d {
	case Byte:
		return "B"
	case KiByte:
		return "KiB"
	case MiByte:
		return "MiB"
	case GiByte:
		return "GiB"
	}
	return ""
}

var DataUnitMapping map[string]DataUnit = map[string]DataUnit{
	"b":    Byte,
	"byte": Byte,

	"k":      KiByte,
	"kb":     KiByte,
	"kib":    KiByte,
	"kibyte": KiByte,

	"m":      MiByte,
	"mb":     MiByte,
	"mib":    MiByte,
	"mibyte": MiByte,

	"g":      GiByte,
	"gb":     GiByte,
	"gib":    GiByte,
	"gibyte": GiByte,
}

func ParseSize(source string) (DataSize, error) {
	trimmed := strings.TrimSpace(source)
	result := DataSize{}
	if len(trimmed) == 0 {
		return result, errors.New("Empty string passed as data size")
	}

	idx := 0
	for idx < len(trimmed) {
		if (trimmed[idx] < '0' || trimmed[idx] > '9') && trimmed[idx] != '.' {
			break
		}
		idx++
	}

	amountString := trimmed[:idx]
	unitString := "B"
	if idx < len(trimmed) {
		unitString = strings.TrimSpace(trimmed[idx:])
	}

	unit, ok := DataUnitMapping[strings.ToLower(unitString)]
	if !ok {
		return result, fmt.Errorf("Unknown data unit: %s", unitString)
	}

	result.Unit = unit

	size, err := strconv.ParseFloat(amountString, 64)
	if err != nil {
		return result, fmt.Errorf("Failed to parse data amount: %w", err)
	}

	if size < 0 {
		return result, errors.New("Negative data amount")
	}

	result.ByteAmount = uint64(size * float64(result.Unit))
	return result, nil
}

func SaneDefaults() Config {
	return Config{
		Workers:       10,
		Insecure:      false,
		Version:       false,
		Help:          false,
		List:          false,
		DumpOnFailure: false,
		Unrestricted:  false,
		LogLevel:      slog.LevelWarn,
		Format:        "std",
		Out:           "",
		MaxCacheSize:  DataSize{ByteAmount: 5 << 20, Unit: MiByte},
		CacheTimeout:  10 * time.Second,
		LuaPaths:      []string{},
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
