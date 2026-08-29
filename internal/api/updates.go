package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/starhui-dev/aster-dns/internal/httpx"
)

const githubLatestReleaseEndpoint = "https://api.github.com/repos/starhui-dev/aster-dns/releases/latest"

// UpdateChecker retrieves the latest published application release.
type UpdateChecker interface {
	LatestRelease(context.Context) (UpdateRelease, error)
}

type UpdateRelease struct {
	Version string
	URL     string
}

type githubUpdateChecker struct {
	client *http.Client
}

func NewGitHubUpdateChecker(client *http.Client) UpdateChecker {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return githubUpdateChecker{client: client}
}

func (checker githubUpdateChecker) LatestRelease(ctx context.Context) (UpdateRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseEndpoint, nil)
	if err != nil {
		return UpdateRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "aster-dns")

	response, err := checker.client.Do(request)
	if err != nil {
		return UpdateRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return UpdateRelease{}, fmt.Errorf("release API returned %s", response.Status)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return UpdateRelease{}, err
	}
	version := strings.TrimSpace(payload.TagName)
	if version == "" {
		return UpdateRelease{}, fmt.Errorf("release API returned an empty version")
	}
	releaseURL := payload.HTMLURL
	if !strings.HasPrefix(releaseURL, "https://github.com/starhui-dev/aster-dns/releases/") {
		releaseURL = "https://github.com/starhui-dev/aster-dns/releases/latest"
	}
	return UpdateRelease{Version: version, URL: releaseURL}, nil
}

type updateResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url"`
}

func updateHandler(build BuildInfo, checker UpdateChecker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if checker == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "upstream", "Update checking is unavailable.", nil)
			return
		}
		release, err := checker.LatestRelease(r.Context())
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadGateway, "upstream", "Unable to check for updates.", nil)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, updateResponse{
			CurrentVersion:  build.Version,
			LatestVersion:   release.Version,
			UpdateAvailable: newerVersion(build.Version, release.Version),
			ReleaseURL:      release.URL,
		})
	}
}

func newerVersion(current, latest string) bool {
	latestParts, latestOK := versionParts(latest)
	if !latestOK {
		return false
	}
	if strings.TrimSpace(current) == "dev" {
		return true
	}
	currentParts, currentOK := versionParts(current)
	if !currentOK {
		return false
	}
	for index := range currentParts {
		if latestParts[index] != currentParts[index] {
			return latestParts[index] > currentParts[index]
		}
	}
	return false
}

func versionParts(value string) ([3]int, bool) {
	var result [3]int
	normalized := strings.TrimSpace(strings.TrimPrefix(value, "v"))
	if separator := strings.IndexAny(normalized, "+-"); separator >= 0 {
		normalized = normalized[:separator]
	}
	parts := strings.Split(normalized, ".")
	if len(parts) != len(result) {
		return result, false
	}
	for index, part := range parts {
		if part == "" {
			return result, false
		}
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return result, false
		}
		result[index] = parsed
	}
	return result, true
}
