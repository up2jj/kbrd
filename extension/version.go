package extension

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const developmentBrowserVersion = "0.0.0.1"

var gitDescribePattern = regexp.MustCompile(
	`^v?[0-9]+\.[0-9]+\.[0-9]+-([0-9]+)-g[0-9a-fA-F]+(-dirty)?$`,
)

func versionedManifest(data []byte, appVersion string) ([]byte, error) {
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode embedded extension manifest: %w", err)
	}

	manifest["version"], manifest["version_name"] = browserVersion(appVersion)
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode versioned extension manifest: %w", err)
	}
	return append(encoded, '\n'), nil
}

// browserVersion returns Chromium's numeric version and the original kbrd
// version used for display.
func browserVersion(appVersion string) (string, string) {
	versionName := strings.TrimSpace(appVersion)
	if versionName == "" {
		versionName = "dev"
	}

	parts, ok := releaseVersionParts(versionName)
	if !ok {
		return developmentBrowserVersion, versionName
	}
	if distance := gitDescribeDistance(versionName); distance != "" {
		parts = append(parts, distance)
	}

	version := strings.Join(parts, ".")
	if version == "0.0.0" {
		version = developmentBrowserVersion
	}
	return version, versionName
}

func releaseVersionParts(version string) ([]string, bool) {
	version = strings.TrimPrefix(version, "v")
	core, _, _ := strings.Cut(version, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return nil, false
	}
	for _, part := range parts {
		if !validBrowserVersionPart(part) {
			return nil, false
		}
	}
	return parts, true
}

func gitDescribeDistance(version string) string {
	match := gitDescribePattern.FindStringSubmatch(version)
	if match == nil || match[1] == "0" || !validBrowserVersionPart(match[1]) {
		return ""
	}
	return match[1]
}

func validBrowserVersionPart(part string) bool {
	if part == "" || len(part) > 1 && part[0] == '0' {
		return false
	}
	_, err := strconv.ParseUint(part, 10, 16)
	return err == nil
}
