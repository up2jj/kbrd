package frontmatter

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Patch describes a formatting-preserving top-level frontmatter mutation.
// Set values are already encoded as inline YAML scalars or flow collections.
type Patch struct {
	Set   map[string]string
	Unset []string
}

// EncodeValue renders a supported TOML/config value as one inline YAML value
// suitable for Set. Sequences are emitted in flow form so the formatting-
// preserving line mutator does not need to own multiline YAML generation.
func EncodeValue(value any) (string, error) {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return "", err
	}
	if node.Kind == yaml.SequenceNode || node.Kind == yaml.MappingNode {
		node.Style |= yaml.FlowStyle
	}
	data, err := yaml.Marshal(&node)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// Apply applies a patch to raw card content. It validates the candidate block
// before returning so malformed existing frontmatter is never made worse by a
// preset or other bulk metadata operation.
func Apply(raw string, patch Patch) (string, error) {
	if err := validatePatch(patch); err != nil {
		return "", err
	}
	if strings.HasPrefix(raw, "---\n") {
		if _, _, fenced := Split(raw); !fenced {
			return "", fmt.Errorf("unterminated leading frontmatter block")
		}
	}

	updated := raw
	for _, key := range patch.Unset {
		updated = Delete(updated, key)
	}
	keys := make([]string, 0, len(patch.Set))
	for key := range patch.Set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		updated = Set(updated, key, patch.Set[key])
	}

	block, _, fenced := Split(updated)
	if fenced {
		if _, err := Parse([]byte(block)); err != nil {
			return "", fmt.Errorf("resulting frontmatter is invalid: %w", err)
		}
	}
	return updated, nil
}

func validatePatch(patch Patch) error {
	unset := make(map[string]struct{}, len(patch.Unset))
	for _, key := range patch.Unset {
		if err := validatePatchKey(key); err != nil {
			return err
		}
		if _, ok := unset[key]; ok {
			return fmt.Errorf("duplicate unset key %q", key)
		}
		unset[key] = struct{}{}
	}
	for key, value := range patch.Set {
		if err := validatePatchKey(key); err != nil {
			return err
		}
		if _, ok := unset[key]; ok {
			return fmt.Errorf("key %q appears in both set and unset", key)
		}
		if err := Validate(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return nil
}

func validatePatchKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("frontmatter key is required")
	}
	if strings.TrimSpace(key) != key || strings.ContainsAny(key, "\r\n:") {
		return fmt.Errorf("frontmatter key %q contains unsupported whitespace or punctuation", key)
	}
	return nil
}
