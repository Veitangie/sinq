// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

import (
	"encoding/json"
	"fmt"
	"io"
)

func ParseConfig(target *ScenarioConfig, source io.Reader) error {
	bytes, err := io.ReadAll(source)
	if err != nil {
		return fmt.Errorf("An error occurred when parsing scenario config: %w", err)
	}
	return json.Unmarshal(bytes, target)
}
