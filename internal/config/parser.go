// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package config

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/Veitangie/sinq/internal/envs"
	"github.com/spf13/pflag"
)

type Parser struct {
	result  *Config
	flagSet *pflag.FlagSet
	writer  io.Writer
}

func setupReporterConfig(flagSet *pflag.FlagSet, result *ReporterConfig) {
	flagSet.BoolVarP(&result.Verbose, "verbose", "V", false, "-V")
	flagSet.VarP(WhenColorValue{&result.Color}, "color", "c", "-c always")
	flagSet.VarP(WhatShowValue{&result.Show}, "show", "S", "-S all")
}

func setupTreewalkerConfig(flagSet *pflag.FlagSet, result *TreewalkerConfig) {
	flagSet.StringVar(&result.SecretsFile, "secrets-file", "", "--secrets-file path/to/file")
	flagSet.VarP(&EnvMapValue{result.Env, false}, "env", "e", "-e key=value")
	flagSet.VarP(&EnvMapValue{result.Secrets, true}, "secret", "s", "-s key=value")
}

func NewParser(writer io.Writer) Parser {
	result := SaneDefaults()
	flagSet := pflag.NewFlagSet("sinq", pflag.ContinueOnError)
	flagSet.SetOutput(writer)

	flagSet.VarP(PositiveIntValue{&result.Workers}, "workers", "w", "-w 100")
	flagSet.BoolVarP(&result.Insecure, "insecure", "i", false, "-i")
	flagSet.BoolVarP(&result.Version, "version", "v", false, "-v")
	flagSet.BoolVarP(&result.Help, "help", "h", false, "-h")
	flagSet.BoolVarP(&result.List, "list", "l", false, "-l")
	flagSet.BoolVar(&result.Completion, "completion", false, "--completion")
	flagSet.MarkHidden("completion") //nolint:errcheck
	flagSet.BoolVar(&result.DumpOnFailure, "dump-on-failure", false, "--dump-on-failure")
	flagSet.BoolVarP(&result.Unrestricted, "unrestricted", "u", false, "-u")
	flagSet.BoolVarP(&result.Print, "print", "p", false, "-p")
	flagSet.BoolVar(&result.NoSpinner, "no-spinner", false, "--no-spinner")

	flagSet.VarP(LogLevelValue{&result.LogLevel}, "log-level", "L", "-L debug")
	flagSet.VarP(FormatValue{&result.Format}, "format", "f", "-f junit")
	flagSet.StringVarP(&result.Out, "out", "o", "", "-o path/to/file.out")
	flagSet.Var(DataSizeValue{&result.MaxCacheSize}, "max-cache-size", "--max-cache-size 10MiB")
	flagSet.Var(PositiveDurationValue{&result.CacheTimeout}, "cache-timeout", "--cache-timeout 30s")
	flagSet.Var(LuaPathsValue{&result.LuaPaths}, "plugins", "--plugins /path/to/lua/dir")
	flagSet.StringSliceVarP(&result.TagsInclude, "tag", "t", result.TagsInclude, "-t goodTag")
	flagSet.StringSliceVar(&result.TagsExclude, "no-tag", result.TagsExclude, "--no-tag badTag")
	flagSet.VarP(RegexSliceValue{&result.NamesInclude}, "name", "n", "-n '.+GoodName.+")
	flagSet.Var(RegexSliceValue{&result.NamesExclude}, "no-name", "--no-name '.+BadName.+'")

	setupTreewalkerConfig(flagSet, &result.Treewalker)
	setupReporterConfig(flagSet, &result.Reporter)

	return Parser{&result, flagSet, writer}
}

func (p *Parser) Parse(arguments []string) error {
	err := p.flagSet.Parse(arguments)
	if err != nil && err != pflag.ErrHelp {
		return err
	}

	p.result.Paths = p.flagSet.Args()
	return nil
}

func (p *Parser) Result() Config {
	return *p.result
}

func parseKeyVal(target map[string]any, key, value string) {
	keySlice := strings.Split(key, ".")
	for _, key := range keySlice[:len(keySlice)-1] {
		if found, ok := target[key]; ok {
			if typedFound, ok := found.(map[string]any); ok {
				target = typedFound
				continue
			}
		}

		newMap := map[string]any{}
		target[key] = newMap
		target = newMap
	}

	var maybeValue any
	err := json.Unmarshal([]byte(value), &maybeValue)
	if err != nil {
		target[keySlice[len(keySlice)-1]] = value
	} else {
		valueTable, valueIsTable := maybeValue.(map[string]any)
		existingTable, existingIsTable := target[keySlice[len(keySlice)-1]].(map[string]any)
		if valueIsTable && existingIsTable {
			envs.DeepMerge(existingTable, valueTable)
		} else {
			target[keySlice[len(keySlice)-1]] = maybeValue
		}
	}
}
