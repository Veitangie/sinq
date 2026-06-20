// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type envMatrixHelper struct {
	EnvMatrix []map[string]any `json:"env_matrix"`
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
		bodySize, err := parseSize(target.MaxBody)
		if err != nil {
			return fmt.Errorf("Failed to parse max body size: %w", err)
		}
		target.MaxBodySize = bodySize
	}

	helper := envMatrixHelper{}
	err = json.Unmarshal(bytes, &helper)
	if err != nil {
		return fmt.Errorf("Failed to parse env matrix: %w", err)
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

	return nil
}

var dataUnitMapping map[string]DataUnit = map[string]DataUnit{
	"B":    Byte,
	"Byte": Byte,

	"K":      KiByte,
	"KiB":    KiByte,
	"KiByte": KiByte,

	"M":      MiByte,
	"MiB":    MiByte,
	"MiByte": MiByte,

	"G":      GiByte,
	"GiB":    GiByte,
	"GiByte": GiByte,
}

func parseSize(source string) (DataSize, error) {
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

	unit, ok := dataUnitMapping[unitString]
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
