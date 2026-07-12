package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/mod/semver"
)

const (
	githubLatestReleaseURL = "https://api.github.com/repos/up2jj/kbrd/releases/latest"
	releaseCheckTimeout    = 3 * time.Second
	releaseResponseLimit   = 1 << 20
	releaseUpdateCellID    = -10
)

// releaseChecker looks up the latest stable GitHub release. It is deliberately
// small and injectable so the TUI owns only scheduling and presentation.
type releaseChecker struct {
	client   *http.Client
	endpoint string
	timeout  time.Duration
}

type releaseCheckMsg struct {
	version string
	url     string
}

func newReleaseChecker() releaseChecker {
	return releaseChecker{
		client:   http.DefaultClient,
		endpoint: githubLatestReleaseURL,
		timeout:  releaseCheckTimeout,
	}
}

func (c releaseChecker) command(localVersion string) tea.Cmd {
	if normalizeReleaseVersion(localVersion) == "" {
		return nil
	}
	return func() tea.Msg { return c.check(localVersion) }
}

func (c releaseChecker) check(localVersion string) releaseCheckMsg {
	localVersion = normalizeReleaseVersion(localVersion)
	if localVersion == "" {
		return releaseCheckMsg{}
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = releaseCheckTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	endpoint := c.endpoint
	if endpoint == "" {
		endpoint = githubLatestReleaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return releaseCheckMsg{}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "kbrd/"+localVersion+" update-check")

	client := c.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return releaseCheckMsg{}
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return releaseCheckMsg{}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, releaseResponseLimit+1))
	if err != nil || len(body) > releaseResponseLimit {
		return releaseCheckMsg{}
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return releaseCheckMsg{}
	}

	latestVersion := normalizeReleaseVersion(release.TagName)
	if latestVersion == "" || release.HTMLURL == "" || semver.Compare(latestVersion, localVersion) <= 0 {
		return releaseCheckMsg{}
	}
	return releaseCheckMsg{version: latestVersion, url: release.HTMLURL}
}

func normalizeReleaseVersion(version string) string {
	version = strings.TrimSpace(version)
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !semver.IsValid(version) {
		return ""
	}
	return version
}

func (b *Board) handleReleaseCheck(msg releaseCheckMsg) tea.Cmd {
	if msg.version == "" {
		return nil
	}
	b.updateVersion = msg.version
	return b.notifier.Info("update available: " + msg.version + " — " + msg.url)
}
