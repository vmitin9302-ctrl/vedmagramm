package gifsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	// klipySearchURL is the documented Klipy search endpoint; the single verb
	// takes the app key.
	klipySearchURL = "https://api.klipy.com/api/v1/%s/gifs/search"
	// klipyMaxBody caps how much of a provider response is decoded.
	klipyMaxBody = 4 << 20
)

type klipySearcher struct {
	client  *http.Client
	apiKey  string
	baseURL string // overridable in tests
}

func (s *klipySearcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	endpoint := s.baseURL
	if endpoint == "" {
		endpoint = klipySearchURL
	}
	params := url.Values{}
	params.Set("q", query)
	params.Set("page", "1")
	params.Set("per_page", strconv.Itoa(limit))
	params.Set("content_filter", "high")
	params.Set("format_filter", "mp4")

	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf(endpoint, url.PathEscape(s.apiKey))+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("klipy search: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("klipy search: upstream status %d", response.StatusCode)
	}
	var payload klipyResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, klipyMaxBody)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("klipy search: decode: %w", err)
	}
	results := make([]Result, 0, len(payload.Data.Data))
	for _, item := range payload.Data.Data {
		mp4 := item.bestMP4()
		if mp4 == "" {
			continue
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = strings.TrimSpace(item.Slug)
		}
		results = append(results, Result{ID: strings.TrimSpace(item.ID), Title: title, MP4URL: mp4})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// klipyResponse mirrors the documented search response: a top-level result
// flag and a nested data object that carries the items.
type klipyResponse struct {
	Result bool `json:"result"`
	Data   struct {
		Data []klipyItem `json:"data"`
	} `json:"data"`
}

type klipyItem struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
	File  struct {
		HD klipyFormats `json:"hd"`
		MD klipyFormats `json:"md"`
		SM klipyFormats `json:"sm"`
	} `json:"file"`
}

type klipyFormats struct {
	MP4 klipyMedia `json:"mp4"`
	GIF klipyMedia `json:"gif"`
}

type klipyMedia struct {
	URL string `json:"url"`
}

// bestMP4 prefers the medium rendition: the HD MP4 often exceeds the
// external-media byte cap, while MD is still large enough for the GIF tab.
func (i klipyItem) bestMP4() string {
	for _, formats := range []klipyFormats{i.File.MD, i.File.SM, i.File.HD} {
		if url := strings.TrimSpace(formats.MP4.URL); url != "" {
			return url
		}
	}
	return ""
}
