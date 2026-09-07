package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oops1/gogit/internal/i18n"
	"github.com/oops1/gogit/internal/version"
)

const (
	latestReleaseAPIURL  = "https://api.github.com/repos/oops1/gogit/releases/latest"
	latestReleasePageURL = "https://github.com/oops1/gogit/releases/latest"
	updateCheckTimeout   = 15 * time.Second
	updateBodyLimit      = 1 << 20
)

var ErrReleaseUnavailable = errors.New("app: the release feed answered with an error")

var updateWG sync.WaitGroup

var (
	releaseFeedURL = latestReleaseAPIURL
	fetchRelease   = downloadReleaseFrom
)

type releaseInfo struct {
	Tag  string `json:"tag_name"`
	Page string `json:"html_url"`
}

func (a *App) checkForUpdates() {
	current := version.String()
	updateWG.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		latest, err := fetchRelease(ctx, releaseFeedURL)
		a.Post(func() { a.reportUpdate(current, latest, err) })
	})
}

func (a *App) reportUpdate(current string, latest releaseInfo, err error) {
	title := i18n.T("Dialog.Update.Title")
	if err != nil {
		a.log.Warn("check for updates failed", "error", err)
		a.showError(title, i18n.Tf("Dialog.Update.Failed", err.Error()))
		return
	}
	if !isNewerRelease(current, latest.Tag) {
		a.showInfo(title, i18n.Tf("Dialog.Update.UpToDate", current))
		return
	}
	page := latest.Page
	if page == "" {
		page = latestReleasePageURL
	}
	a.askConfirm(title, i18n.Tf("Dialog.Update.Available", latest.Tag, current), func(ok bool) {
		if ok {
			a.openBrowser(page)
		}
	})
}

func downloadReleaseFrom(ctx context.Context, rawURL string) (releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", remoteUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("%w: %s", ErrReleaseUnavailable, resp.Status)
	}
	var info releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, updateBodyLimit)).Decode(&info); err != nil {
		return releaseInfo{}, err
	}
	return info, nil
}

func isNewerRelease(current, latest string) bool {
	if latest == "" {
		return false
	}
	return compareVersions(latest, current) > 0
}

func compareVersions(a, b string) int {
	left, right := versionNumbers(a), versionNumbers(b)
	for i := 0; i < len(left) || i < len(right); i++ {
		if diff := numberAt(left, i) - numberAt(right, i); diff != 0 {
			if diff < 0 {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionNumbers(text string) []int {
	trimmed := strings.TrimPrefix(strings.TrimSpace(text), "v")
	if cut := strings.IndexAny(trimmed, "-+"); cut >= 0 {
		trimmed = trimmed[:cut]
	}
	parts := strings.Split(trimmed, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return numbers
		}
		numbers = append(numbers, n)
	}
	return numbers
}

func numberAt(numbers []int, i int) int {
	if i < len(numbers) {
		return numbers[i]
	}
	return 0
}
