package app

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
)

const releaseAtomURL = "https://github.com/oops1/GoGit/releases.atom"

var ErrReleaseRateLimited = errors.New("app: github answered that the request quota is spent")

var fetchReleaseFeed = downloadReleaseAtom

type releaseAtom struct {
	Entries []struct {
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func rateLimited(resp *http.Response) bool {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return false
	}
	return resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""
}

func downloadReleaseAtom(ctx context.Context, rawURL string) (releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/atom+xml")
	req.Header.Set("User-Agent", remoteUserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, errorOfStatus(resp)
	}
	var feed releaseAtom
	if err := xml.NewDecoder(io.LimitReader(resp.Body, updateBodyLimit)).Decode(&feed); err != nil {
		return releaseInfo{}, err
	}
	if len(feed.Entries) == 0 {
		return releaseInfo{}, ErrReleaseUnavailable
	}
	first := feed.Entries[0]
	return releaseInfo{Tag: strings.TrimSpace(first.Title), Page: first.Link.Href}, nil
}

func latestRelease(ctx context.Context) (releaseInfo, error) {
	info, feedErr := fetchReleaseFeed(ctx, releaseAtomURL)
	if feedErr == nil {
		return info, nil
	}
	fallback, apiErr := fetchRelease(ctx, releaseFeedURL)
	if apiErr == nil {
		return fallback, nil
	}
	if errors.Is(apiErr, ErrReleaseRateLimited) {
		return releaseInfo{}, apiErr
	}
	return releaseInfo{}, feedErr
}
