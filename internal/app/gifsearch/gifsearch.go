// Package gifsearch implements the external search backends behind the inline
// @gif bot: a provider client (Klipy or Giphy) plus a cache that mints stored
// GIFv documents, so that inline results are ordinary documents the clients can
// send without any client-side change.
package gifsearch

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"telesrv/internal/domain"
)

const (
	// MaxLimit is the protocol ceiling for bot inline results.
	MaxLimit = domain.MaxBotInlineResults
	// mintConcurrency bounds parallel document downloads per query.
	mintConcurrency = 4
	// cacheMaxEntries bounds the number of cached queries.
	cacheMaxEntries = 256
)

// Searcher returns candidate animations for one @gif query.
type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]Result, error)
}

// Result is one provider hit: a stable id, a human title and a direct MP4 URL.
type Result struct {
	ID     string
	Title  string
	MP4URL string
}

// DocumentMinter turns a remote animation into a stored, sendable GIFv document.
type DocumentMinter interface {
	CreateGifFromURL(ctx context.Context, rawURL, title string) (domain.Document, error)
}

// NewSearcher builds the backend selected by provider. "off" and an empty
// provider name return (nil, nil): the inline bot then keeps using its local
// catalog only.
func NewSearcher(provider, apiKey string, client *http.Client) (Searcher, error) {
	name := strings.ToLower(strings.TrimSpace(provider))
	if name == "" || name == "off" {
		return nil, nil
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return nil, fmt.Errorf("gif search provider %q requires an API key", name)
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	switch name {
	case "klipy":
		return &klipySearcher{client: client, apiKey: key}, nil
	case "giphy":
		return &giphySearcher{client: client, apiKey: key}, nil
	default:
		return nil, fmt.Errorf("unknown gif search provider %q", name)
	}
}

// Provider searches the configured backend, mints the hits into documents and
// caches the minted set per normalized query, so repeated typing while the user
// refines a query does not download the same animations again.
type Provider struct {
	searcher Searcher
	minter   DocumentMinter
	limit    int
	ttl      time.Duration
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	docs    []domain.Document
	expires time.Time
}

// NewProvider wires a searcher to a document minter. limit is clamped to
// [1,MaxLimit]; ttl <= 0 disables the cache.
func NewProvider(searcher Searcher, minter DocumentMinter, limit int, ttl time.Duration) *Provider {
	if limit < 1 {
		limit = 1
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	return &Provider{
		searcher: searcher,
		minter:   minter,
		limit:    limit,
		ttl:      ttl,
		now:      time.Now,
		cache:    map[string]cacheEntry{},
	}
}

// SearchGIFs returns stored GIFv documents for query. A blank query, a missing
// dependency or zero minted documents all return (nil, nil): the caller then
// falls back to the local catalog instead of failing the inline query.
func (p *Provider) SearchGIFs(ctx context.Context, query string) ([]domain.Document, error) {
	if p == nil || p.searcher == nil || p.minter == nil {
		return nil, nil
	}
	key := strings.ToLower(strings.TrimSpace(query))
	if key == "" {
		return nil, nil
	}
	if docs := p.lookup(key); len(docs) > 0 {
		return docs, nil
	}
	results, err := p.searcher.Search(ctx, key, p.limit)
	if err != nil {
		return nil, err
	}
	docs := p.mintAll(ctx, results)
	if len(docs) > 0 {
		p.store(key, docs)
	}
	return docs, nil
}

// mintAll downloads and stores the provider hits in parallel, preserving the
// provider order. Individual failures are skipped: a partial result set is
// still better than an empty GIF tab.
func (p *Provider) mintAll(ctx context.Context, results []Result) []domain.Document {
	if len(results) == 0 {
		return nil
	}
	docs := make([]domain.Document, len(results))
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(mintConcurrency)
	for i, result := range results {
		i, result := i, result
		if strings.TrimSpace(result.MP4URL) == "" {
			continue
		}
		group.Go(func() error {
			doc, err := p.minter.CreateGifFromURL(groupCtx, result.MP4URL, result.Title)
			if err != nil {
				return nil
			}
			docs[i] = doc
			return nil
		})
	}
	_ = group.Wait()
	out := make([]domain.Document, 0, len(docs))
	for _, doc := range docs {
		if doc.ID != 0 {
			out = append(out, doc)
		}
	}
	return out
}

func (p *Provider) lookup(key string) []domain.Document {
	if p.ttl <= 0 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cache[key]
	if !ok {
		return nil
	}
	if p.now().After(entry.expires) {
		delete(p.cache, key)
		return nil
	}
	return entry.docs
}

func (p *Provider) store(key string, docs []domain.Document) {
	if p.ttl <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cache) >= cacheMaxEntries {
		now := p.now()
		for k, entry := range p.cache {
			if now.After(entry.expires) {
				delete(p.cache, k)
			}
		}
		if len(p.cache) >= cacheMaxEntries {
			p.cache = map[string]cacheEntry{}
		}
	}
	p.cache[key] = cacheEntry{docs: docs, expires: p.now().Add(p.ttl)}
}
