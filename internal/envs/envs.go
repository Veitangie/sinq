// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package envs

func DeepMerge(mut, immut map[string]any) {
	if mut == nil || immut == nil {
		return
	}

	for k, v := range immut {
		switch val := v.(type) {
		case map[string]any:
			if next, ok := mut[k].(map[string]any); ok {
				DeepMerge(next, val)
			} else {
				mut[k] = DeepCopy(val)
			}
		case []any:
			sliceCopy := make([]any, len(val))
			copy(sliceCopy, val)
			mut[k] = sliceCopy
		default:
			mut[k] = v
		}
	}
}

func DeepCopy(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))

	for k, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[k] = DeepCopy(val)
		case []any:
			sliceCopy := make([]any, len(val))
			copy(sliceCopy, val)
			dst[k] = sliceCopy
		default:
			dst[k] = v
		}
	}
	return dst
}
