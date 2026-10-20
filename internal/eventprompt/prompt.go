// Package eventprompt reads the host's before_agent_start prompt contract.
package eventprompt

import "fmt"

// Read supports PiG 0.5's lazy prompt and legacy string event fixtures. Reading
// through Get preserves host prompt composition and propagates resolution errors.
func Read(source string, data map[string]any) (string, error) {
	switch prompt := data["systemPrompt"].(type) {
	case interface{ Get() (string, error) }:
		text, err := prompt.Get()
		if err != nil {
			return "", fmt.Errorf("%s: resolve before_agent_start systemPrompt: %w", source, err)
		}
		return text, nil
	case string:
		return prompt, nil
	default:
		return "", fmt.Errorf("%s: before_agent_start has no readable systemPrompt", source)
	}
}
