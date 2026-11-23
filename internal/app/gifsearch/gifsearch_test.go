package gifsearch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"telesrv/internal/domain"
)

func TestNewSearcherSelectsProvider(t *testing.T) {
	if searcher, err := NewSearcher("off", "key", nil); err != nil || searcher != nil {
		t.Fatalf("off provider = (%v, %v), want (nil, nil)", searcher, err)
	}
	if _, err := NewSearcher("klipy", "  ", nil); err == nil {
		t.Fatal("empty key must be rejected for an enabled provider")
	}
	searcher, err := NewSearcher("KLIPY", "key", nil)
	if err != nil {
		t.Fatalf("klipy provider: %v", err)
	}
	if _, ok := searcher.(*klipySearcher); !ok {
		t.Fatalf("klipy provider type = %T", searcher)
	}
	if _, err := NewSearcher("nope", "key", nil); err == nil {
		t.Fatal("unknown provider must be rejected")
	}
}

func TestKlipySearchParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "cat" {
			t.Errorf("q = %q, want cat", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "2" {
			t.Errorf("per_page = %q, want 2", got)
		}
		if !strings.Contains(r.URL.Path, "/api/v1/test-key/gifs/search") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":true,"data":{"data":[
			{"id":"a1","slug":"cat-one","title":"Cat One","file":{"hd":{"mp4":{"url":"https://cdn.test/a1.mp4"}},"md":{"mp4":{"url":"https://cdn.test/a1-md.mp4"}}}},
			{"id":"a2","slug":"cat-two","title":"","file":{"sm":{"mp4":{"url":"https://cdn.test/a2.mp4"}}}}
		]}}`))
	}))
	defer server.Close()

	searcher := &klipySearcher{
		client:  server.Client(),
		apiKey:  "test-key",
		baseURL: server.URL + "/api/v1/%s/gifs/search",
	}
	results, err := searcher.Search(context.Background(), "cat", 2)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].MP4URL != "https://cdn.test/a1-md.mp4" || results[0].Title != "Cat One" {
		t.Errorf("first result = %+v", results[0])
	}
	if results[1].MP4URL != "https://cdn.test/a2.mp4" || results[1].Title != "cat-two" {
		t.Errorf("second result = %+v", results[1])
	}
}

func TestGiphySearchParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api_key"); got != "test-key" {
			t.Errorf("api_key = %q", got)
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"g1","title":"Dog","images":{"original":{"mp4":"https://cdn.test/g1.mp4"},"downsized":{"mp4":"https://cdn.test/g1-small.mp4"}}},
			{"id":"g2","title":"No mp4","images":{}}
		]}`))
	}))
	defer server.Close()

	searcher := &giphySearcher{client: server.Client(), apiKey: "test-key", baseURL: server.URL + "/v1/gifs/search"}
	results, err := searcher.Search(context.Background(), "dog", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].MP4URL != "https://cdn.test/g1-small.mp4" {
		t.Errorf("mp4 = %q", results[0].MP4URL)
	}
}

func TestProviderCachesMintedDocuments(t *testing.T) {
	searcher := &fakeSearcher{results: []Result{
		{ID: "1", Title: "A", MP4URL: "https://cdn.test/1.mp4"},
		{ID: "2", Title: "B", MP4URL: "https://cdn.test/2.mp4"},
	}}
	minter := &fakeMinter{}
	provider := NewProvider(searcher, minter, 12, time.Hour)

	first, err := provider.SearchGIFs(context.Background(), "  Cat ")
	if err != nil {
		t.Fatalf("first search: %v", err)
	}
	if len(first) != 2 || minter.count() != 2 || searcher.count() != 1 {
		t.Fatalf("first = %d docs, %d mints, %d searches", len(first), minter.count(), searcher.count())
	}

	second, err := provider.SearchGIFs(context.Background(), "cat")
	if err != nil || len(second) != 2 {
		t.Fatalf("cached search = (%d, %v)", len(second), err)
	}
	if minter.count() != 2 || searcher.count() != 1 {
		t.Fatalf("cache miss: %d mints, %d searches", minter.count(), searcher.count())
	}
}

func TestProviderSkipsBlankQueryAndMissingDependencies(t *testing.T) {
	provider := NewProvider(&fakeSearcher{}, &fakeMinter{}, 4, time.Hour)
	if docs, err := provider.SearchGIFs(context.Background(), "   "); err != nil || docs != nil {
		t.Fatalf("blank query = (%v, %v)", docs, err)
	}
	if docs, err := NewProvider(nil, nil, 4, time.Hour).SearchGIFs(context.Background(), "cat"); err != nil || docs != nil {
		t.Fatalf("nil deps = (%v, %v)", docs, err)
	}
}

func TestProviderSkipsFailedMints(t *testing.T) {
	searcher := &fakeSearcher{results: []Result{
		{ID: "1", Title: "ok", MP4URL: "https://cdn.test/1.mp4"},
		{ID: "2", Title: "bad", MP4URL: "https://cdn.test/bad.mp4"},
	}}
	minter := &fakeMinter{failURL: "https://cdn.test/bad.mp4"}
	provider := NewProvider(searcher, minter, 12, 0)

	docs, err := provider.SearchGIFs(context.Background(), "cat")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(docs) != 1 || docs[0].MimeType != "video/mp4" {
		t.Fatalf("docs = %+v", docs)
	}
}

type fakeSearcher struct {
	mu      sync.Mutex
	calls   int
	results []Result
	err     error
}

func (f *fakeSearcher) Search(context.Context, string, int) ([]Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.results, f.err
}

func (f *fakeSearcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeMinter struct {
	mu      sync.Mutex
	calls   int
	failURL string
	err     error
}

func (f *fakeMinter) CreateGifFromURL(_ context.Context, rawURL, _ string) (domain.Document, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return domain.Document{}, f.err
	}
	if rawURL == f.failURL {
		return domain.Document{}, errors.New("download failed")
	}
	return domain.Document{ID: 1, MimeType: "video/mp4"}, nil
}

func (f *fakeMinter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}
