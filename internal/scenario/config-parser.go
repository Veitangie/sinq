// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/envs"
)

type configHelper struct {
	Env       map[string]any   `json:"env"`
	EnvMatrix []map[string]any `json:"env_matrix"`
	Tags      []string         `json:"tags"`
}

func parseAdditionalData(target *ScenarioConfig, data []byte) error {
	helper := configHelper{}
	err := json.Unmarshal(data, &helper)
	if err != nil {
		return jsonError(data, fmt.Errorf("Failed to parse scenario config: %w", err))
	}

	if len(helper.EnvMatrix) > 0 {
		invalidKeys := []string{}
		allTypeSafe := make([]map[string]map[string]any, 0, len(helper.EnvMatrix))
		for idx, env := range helper.EnvMatrix {
			if len(env) == 0 {
				continue
			}

			typeSafe := make(map[string]map[string]any, len(env))
			for k, v := range env {
				if typeSafeEntry, ok := v.(map[string]any); ok {
					typeSafe[k] = typeSafeEntry
				} else {
					invalidKeys = append(invalidKeys, fmt.Sprintf("[%d].%s", idx, k))
				}
			}

			allTypeSafe = append(allTypeSafe, typeSafe)
		}

		if len(invalidKeys) > 0 {
			return fmt.Errorf("Failed to parse env matrix: keys %v have non-object values", invalidKeys)
		}

		target.EnvMatrix = append(target.EnvMatrix, allTypeSafe...)
	}

	if len(helper.Tags) > 0 {
		for _, tag := range helper.Tags {
			target.Tags[tag] = struct{}{}
		}
	}

	if len(helper.Env) != 0 {
		envs.DeepMerge(target.Env, helper.Env)
	}

	return nil
}

func jsonError(source []byte, err error) error {
	offset := int64(-1)
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntaxErr):
		offset = syntaxErr.Offset
	case errors.As(err, &typeErr):
		offset = typeErr.Offset
	}
	if offset < 0 || offset > int64(len(source)) {
		return err
	}

	before := source[:offset]
	line := bytes.Count(before, []byte{'\n'}) + 1
	column := len(before) - (bytes.LastIndexByte(before, '\n') + 1)
	return PositionError{Line: line, Offset: max(column, 1), Err: err}
}

func ParseConfig(target *ScenarioConfig, source io.Reader) error {
	data, err := io.ReadAll(source)
	if err != nil {
		return fmt.Errorf("Failed to read scenario config: %w", err)
	}

	oldSize := target.MaxBody
	err = json.Unmarshal(data, target)
	if err != nil {
		return jsonError(data, fmt.Errorf("Failed to parse scenario config: %w", err))
	}

	if target.MaxBody != oldSize {
		bodySize, errInSize := config.ParseSize(target.MaxBody)
		if errInSize != nil {
			err = errors.Join(err, fmt.Errorf("Failed to parse max body size: %w", errInSize))
		} else {
			target.MaxBodySize = bodySize
		}
	}

	if target.ReqTimeout.Duration <= 0 {
		err = errors.Join(err, errors.New("Non positive request timeout"))
	}

	if target.ScriptTimeout.Duration <= 0 {
		err = errors.Join(err, errors.New("Non positive script timeout"))
	}

	if target.Timeout.Duration <= 0 {
		err = errors.Join(err, errors.New("Non positive scenario timeout"))
	}

	if target.MaxRetries < 0 {
		err = errors.Join(err, errors.New("Negative retries"))
	}

	if target.MaxRedirects < 0 {
		err = errors.Join(err, errors.New("Negative redirects"))
	}

	err = errors.Join(err, parseAdditionalData(target, data))
	if err != nil {
		return err
	}

	return nil
}
