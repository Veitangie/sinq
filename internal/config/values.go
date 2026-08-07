package config

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

type PositiveIntValue struct{ target *int }

var _ pflag.Value = PositiveIntValue{}

func (v PositiveIntValue) String() string {
	if v.target == nil {
		return "unset"
	}

	return strconv.Itoa(*v.target)
}

func (v PositiveIntValue) Set(value string) error {
	if v.target == nil {
		return errors.New("positive int setter was not initialized")
	}

	maybeValue, err := strconv.Atoi(value)
	if err != nil {
		return err
	}

	if maybeValue <= 0 {
		return errors.New("expected a positive integer")
	}
	*v.target = maybeValue
	return nil
}

func (v PositiveIntValue) Type() string { return "int" }

type NonNegativeDurationValue struct{ target *time.Duration }

var _ pflag.Value = NonNegativeDurationValue{}

func (v NonNegativeDurationValue) String() string {
	if v.target == nil {
		return "unset"
	}

	return v.target.String()
}

func (v NonNegativeDurationValue) Set(value string) error {
	if v.target == nil {
		return errors.New("log level setter was not initialized")
	}

	maybeDuration, err := time.ParseDuration(value)
	if err != nil {
		return err
	}

	if maybeDuration < 0 {
		return errors.New("expected non-negative duration")
	}

	*v.target = maybeDuration
	return nil
}

func (v NonNegativeDurationValue) Type() string { return "duration" }

type LogLevelValue struct{ target *slog.Level }

var _ pflag.Value = LogLevelValue{}

func (v LogLevelValue) String() string {
	if v.target == nil {
		return "unset"
	}

	switch *v.target {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelInfo:
		return "info"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "unknown"
	}
}

func (v LogLevelValue) Set(value string) error {
	if v.target == nil {
		return errors.New("log level setter was not initialized")
	}

	switch strings.ToLower(value) {
	case "debug":
		*v.target = slog.LevelDebug
	case "info":
		*v.target = slog.LevelInfo
	case "warn":
		*v.target = slog.LevelWarn
	case "error":
		*v.target = slog.LevelError
	default:
		return errors.New("expected one of: debug, info, warn, error")
	}
	return nil
}

func (v LogLevelValue) Type() string { return "string" }

type FormatValue struct{ target *string }

var _ pflag.Value = FormatValue{}

func (v FormatValue) String() string {
	if v.target == nil {
		return "unset"
	}

	return *v.target
}

func (v FormatValue) Set(value string) error {
	if v.target == nil {
		return errors.New("format setter was not initialized")
	}

	value = strings.ToLower(value)
	switch value {
	case "std":
	case "junit":
	default:
		return errors.New("expected one of: std, junit")
	}

	*v.target = value
	return nil
}

func (v FormatValue) Type() string { return "string" }

type DataSizeValue struct{ target *DataSize }

var _ pflag.Value = DataSizeValue{}

func (v DataSizeValue) String() string {
	if v.target == nil {
		return "unset"
	}

	return v.target.String()
}

func (v DataSizeValue) Set(value string) error {
	if v.target == nil {
		return errors.New("data size setter was not initialized")
	}

	res, err := ParseSize(value)
	if err != nil {
		return err
	}
	*v.target = res
	return nil
}

func (v DataSizeValue) Type() string { return "string" }

type LuaPathsValue struct{ target *[]string }

var _ pflag.Value = LuaPathsValue{}

func (v LuaPathsValue) String() string {
	if v.target == nil {
		return "unset"
	}

	return strings.Join(*v.target, string(filepath.Separator))
}

func (v LuaPathsValue) Set(value string) error {
	if v.target == nil {
		return errors.New("lua path setter was not initialized")
	}

	*v.target = append(*v.target, filepath.SplitList(value)...)
	return nil
}

func (v LuaPathsValue) Type() string { return "string" }

type EnvMapValue struct {
	target   map[string]any
	isSecret bool
}

var _ pflag.Value = &EnvMapValue{}

func (v *EnvMapValue) String() string {
	if v.target == nil {
		return "unset"
	}

	if len(v.target) == 0 {
		return "{}"
	}
	if v.isSecret {
		return "{ *** }"
	}
	return "{...}"
}

func (v *EnvMapValue) Set(value string) error {
	if v.target == nil {
		return errors.New("key-value setter was not initialized")
	}

	keyValSlice := strings.SplitN(value, "=", 2)
	representaton := "--env|-e"
	if v.isSecret {
		representaton = "--secret|-s"
	}
	if len(keyValSlice) != 2 {
		return fmt.Errorf("could not split by '='. Usage: %s key=value", representaton)
	}

	if keyValSlice[0] == "" {
		return fmt.Errorf("empty key. Usage: %s key=value", representaton)
	}

	parseKeyVal(v.target, keyValSlice[0], keyValSlice[1])
	return nil
}

func (v *EnvMapValue) Type() string { return "string" }

type WhenColorValue struct{ target *WhenColor }

var _ pflag.Value = WhenColorValue{}

func (v WhenColorValue) String() string {
	if v.target == nil {
		return "unset"
	}

	switch *v.target {
	case Never:
		return "never"
	case Always:
		return "always"
	case Auto:
		return "auto"
	default:
		return "unknown"
	}
}

func (v WhenColorValue) Set(value string) error {
	if v.target == nil {
		return errors.New("color setter was not initialized")
	}

	switch strings.ToLower(value) {
	case "never":
		*v.target = Never
	case "always":
		*v.target = Always
	case "auto":
		*v.target = Auto
	default:
		return errors.New("expected one of: never, auto, always")
	}
	return nil
}

func (v WhenColorValue) Type() string { return "string" }

type WhatShowValue struct{ target *WhatShow }

var _ pflag.Value = WhatShowValue{}

func (v WhatShowValue) String() string {
	if v.target == nil {
		return "unset"
	}

	switch *v.target {
	case All:
		return "all"
	case NoSkip:
		return "no-skip"
	case Failed:
		return "failed"
	default:
		return "unknown"
	}
}

func (v WhatShowValue) Set(value string) error {
	if v.target == nil {
		return errors.New("show setter was not initialized")
	}

	switch strings.ToLower(value) {
	case "all":
		*v.target = All
	case "no-skip":
		*v.target = NoSkip
	case "failed":
		*v.target = Failed
	default:
		return errors.New("expected one of: all, no-skip, failed")
	}
	return nil
}

func (v WhatShowValue) Type() string { return "string" }

type RegexSliceValue struct{ target *[]regexp.Regexp }

var _ pflag.Value = RegexSliceValue{}

func (v RegexSliceValue) String() string {
	if v.target == nil {
		return "unset"
	}

	stringified := make([]string, 0, len(*v.target))
	for _, r := range *v.target {
		stringified = append(stringified, r.String())
	}
	return strings.Join(stringified, ", ")
}

func (v RegexSliceValue) Set(value string) error {
	if v.target == nil {
		return errors.New("regex setter was not initialized")
	}

	nameRegex, err := regexp.Compile(value)
	if err != nil {
		return fmt.Errorf("failed to compile regex for filtering by name: %w", err)
	}
	if nameRegex == nil {
		return errors.New("regex for filtering by name did not compile, but returned no errors")
	}

	*v.target = append(*v.target, *nameRegex)
	return nil
}

func (v RegexSliceValue) Type() string { return "string" }
