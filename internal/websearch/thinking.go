package websearch

import "slices"

var thinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh"}

// Match pi-ai 0.80.3: unsupported effort clamps upward first, then downward.
func thinkingEffort(model Model, requested string) (string, bool) {
	available := func(level string) bool {
		if !slices.Contains(thinkingLevels, level) {
			return false
		}
		if !model.Reasoning {
			return level == "off"
		}
		mapped, present := model.ThinkingMap[level]
		return !(present && mapped == nil) && (level != "xhigh" || present)
	}
	level := requested
	if !available(level) {
		index := slices.Index(thinkingLevels, requested)
		level = "off"
		found := false
		if index >= 0 {
			for i := index; i < len(thinkingLevels); i++ {
				if available(thinkingLevels[i]) {
					level = thinkingLevels[i]
					found = true
					break
				}
			}
			if !found {
				for i := index - 1; i >= 0; i-- {
					if available(thinkingLevels[i]) {
						level = thinkingLevels[i]
						found = true
						break
					}
				}
			}
		}
		if !found {
			for _, candidate := range thinkingLevels {
				if available(candidate) {
					level = candidate
					break
				}
			}
		}
	}
	if level == "off" {
		return "", false
	}
	if mapped := model.ThinkingMap[level]; mapped != nil {
		return *mapped, true
	}
	return level, true
}
