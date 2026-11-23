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
	// giphySearchURL is the public Giphy search endpoint.
	giphySearchURL = "https://api.giphy.com/v1/gifs/search"
	// giphyMaxBody caps how much of a provider response is decoded.
	giphyMaxBody = 4 << 20
)

type giphySearcher struct {
	client  *http.Client
	apiKey  string
	baseURL string // overridable in tests
}

func (s *giphySearcher) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	endpoint := s.baseURL
	if endpoint == "" {
		endpoint = giphySearchURL
	}
	params := url.Values{}
	params.Set("api_key", s.apiKey)
	params.Set("q", query)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", "0")
	params.Set("rating", "pg-13")

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("giphy search: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("giphy search: upstream status %d", response.StatusCode)
	}
	var payload giphyResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, giphyMaxBody)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("giphy search: decode: %w", err)
	}
	results := make([]Result, 0, len(payload.Data))
	for _, item := range payload.Data {
		mp4 := item.bestMP4()
		if mp4 == "" {
			continue
		}
		results = append(results, Result{
			ID: strings.TrimSpace(item.ID), Title: strings.TrimSpace(item.Title), MP4URL: mp4,
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

type giphyResponse struct {
	Data []giphyItem `json:"data"`
}

type giphyItem struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	Images giphyImages `json:"images"`
}

type giphyImages struct {
	Original    giphyImage `json:"original"`
	Downsized   giphyImage `json:"downsized"`
	FixedHeight giphyImage `json:"fixed_height"`
}

type giphyImage struct {
	MP4 string `json:"mp4"`
}

// bestMP4 prefers the smaller renditions first: the original MP4 often exceeds
// the external-media byte cap, while a smaller rendition still looks fine inline.
func (i giphyItem) bestMP4() string {
	for _, image := range []giphyImage{i.Images.FixedHeight, i.Images.Downsized, i.Images.Original} {
		if url := strings.TrimSpace(image.MP4); url != "" {
			return url
		}
	}
	return ""
}
