package files

import (
	"context"
	"strings"

	"telesrv/internal/domain"
)

// CreateGifFromURL fetches a remote animation and stores it as a canonical GIFv
// document, the same shape the @gif catalog and silent MP4 uploads produce. The
// fetch goes through the external-media client, so the SSRF block list, the size
// cap and the per-minute budget apply unchanged.
func (s *Service) CreateGifFromURL(ctx context.Context, rawURL, title string) (domain.Document, error) {
	if s == nil || s.externalMedia == nil {
		return domain.Document{}, ErrExternalMediaDisabled
	}
	if s.gifs == nil {
		return domain.Document{}, domain.ErrGifCatalogFileInvalid
	}
	data, _, err := s.externalMedia.fetch(ctx, rawURL)
	if err != nil {
		return domain.Document{}, err
	}
	if _, ok := detectGifCatalogUploadMime(data); !ok {
		return domain.Document{}, domain.ErrGifCatalogFileInvalid
	}
	name := strings.TrimSpace(title)
	if name == "" {
		name = "animation"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".mp4") {
		name += ".mp4"
	}
	return s.storeGifDocument(ctx, name, data)
}
