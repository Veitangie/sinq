// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Veitangie/sinq/internal/config"
)

type configHelper struct {
	EnvMatrix []map[string]any `json:"env_matrix"`
	Tags      []string         `json:"tags"`
}

func parseAdditionalData(target *ScenarioConfig, bytes []byte) error {
	helper := configHelper{}
	err := json.Unmarshal(bytes, &helper)
	if err != nil {
		return fmt.Errorf("Failed to parse scenario config: %w", err)
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
			target.Tags[tag] = true
		}
	}

	return nil
}

func ParseConfig(target *ScenarioConfig, source io.Reader) error {
	bytes, err := io.ReadAll(source)
	if err != nil {
		return fmt.Errorf("An error occurred when reading scenario config: %w", err)
	}

	oldSize := target.MaxBody
	err = json.Unmarshal(bytes, target)
	if err != nil {
		return fmt.Errorf("An error occurred when parsing scenario config: %w", err)
	}

	if target.MaxBody != oldSize {
		bodySize, err := config.ParseSize(target.MaxBody)
		if err != nil {
			return fmt.Errorf("Failed to parse max body size: %w", err)
		}
		target.MaxBodySize = bodySize
	}

	err = parseAdditionalData(target, bytes)
	if err != nil {
		return err
	}

	return nil
}
