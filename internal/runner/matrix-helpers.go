// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

func buildAllPaths(wholeMap []map[string]map[string]any) ([][]string, int) {
	total := 1
	allLabels := make([][]string, 0, len(wholeMap))
	for _, mat := range wholeMap {
		if len(mat) == 0 {
			continue
		}

		total *= len(mat)
		curLabels := make([]string, 0, len(mat))
		for k := range mat {
			curLabels = append(curLabels, k)
		}
		allLabels = append(allLabels, curLabels)
	}
	return allLabels, total
}

func takePath(path int, allPaths [][]string) []string {
	labels := make([]string, len(allPaths))
	for j := range labels {
		curIdx := len(labels) - j - 1
		labels[curIdx] = allPaths[curIdx][path%len(allPaths[curIdx])]

		path /= len(allPaths[curIdx])
	}
	return labels
}

func deepMerge(mut, immut map[string]any) {
	if mut == nil || immut == nil {
		return
	}

	for k, v := range immut {
		switch val := v.(type) {
		case map[string]any:
			if next, ok := mut[k].(map[string]any); ok {
				deepMerge(next, val)
			} else {
				mut[k] = deepCopy(val)
			}
		default:
			mut[k] = v
		}
	}
}

func deepCopy(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))

	for k, v := range src {
		switch val := v.(type) {
		case map[string]any:
			dst[k] = deepCopy(val)
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
