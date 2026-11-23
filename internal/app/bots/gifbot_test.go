package bots

import (
	"context"
	"testing"

	"telesrv/internal/domain"
)

type gifCatalogTestSource struct {
	entries []domain.GifCatalogEntry
	docs    []domain.Document
}

func (s gifCatalogTestSource) ListGifCatalog(context.Context, bool) ([]domain.GifCatalogEntry, error) {
	return s.entries, nil
}
func (s gifCatalogTestSource) GetDocuments(context.Context, []int64) ([]domain.Document, error) {
	return s.docs, nil
}

func TestGifBotRanksMatchesAndReturnsPlayableDocuments(t *testing.T) {
	doc := domain.Document{ID: 9, MimeType: "video/mp4", Attributes: []domain.DocumentAttribute{{Kind: domain.DocAttrAnimated}, {Kind: domain.DocAttrVideo, W: 320, H: 240, Duration: 1}}}
	svc := NewService(nil, nil, nil, WithGifCatalog(gifCatalogTestSource{
		entries: []domain.GifCatalogEntry{{ID: 1, Title: "Dog", DocumentID: 9}, {ID: 2, Title: "Cat wave", DocumentID: 9}}, docs: []domain.Document{doc},
	}))
	got, handled, err := svc.OnInlineQuery(context.Background(), domain.GifBotUserID, 42, "cat", "")
	if err != nil || !handled || got.QueryID != 0 || len(got.Results) != 2 {
		t.Fatalf("OnInlineQuery = %+v,%v,%v", got, handled, err)
	}
	if got.Results[0].ID != "2" || got.Results[0].Media == nil || got.Results[0].Media.Document.ID != 9 {
		t.Fatalf("ranked results = %+v", got.Results)
	}
}

func TestGifBotFailsFastOnMissingDocument(t *testing.T) {
	svc := NewService(nil, nil, nil, WithGifCatalog(gifCatalogTestSource{entries: []domain.GifCatalogEntry{{ID: 1, Title: "Missing", DocumentID: 99}}}))
	if _, handled, err := svc.OnInlineQuery(context.Background(), domain.GifBotUserID, 42, "", ""); !handled || err == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
}

func TestGifBotRanksManualCategoryAfterTitleMatches(t *testing.T) {
	entries := []domain.GifCatalogEntry{
		{ID: 1, Title: "Unrelated", Category: domain.GifCategoryAnimals, CategoryManual: true},
		{ID: 2, Title: "Cat wave", Category: domain.GifCategoryReaction},
		{ID: 3, Title: "Other", Category: domain.GifCategoryOther},
	}
	got := rankGifCatalogEntries(entries, "cat")
	if got[0].ID != 2 || got[1].ID != 1 || got[2].ID != 3 {
		t.Fatalf("ranked IDs = %d,%d,%d", got[0].ID, got[1].ID, got[2].ID)
	}
}

type gifSearchTestSource struct {
	docs  []domain.Document
	calls int
}

func (s *gifSearchTestSource) SearchGIFs(context.Context, string) ([]domain.Document, error) {
	s.calls++
	return s.docs, nil
}

func TestGifBotPrefersExternalSearch(t *testing.T) {
	doc := domain.Document{
		ID: 77, MimeType: "video/mp4",
		Attributes: []domain.DocumentAttribute{
			{Kind: domain.DocAttrFilename, FileName: "cat dance.mp4"},
			{Kind: domain.DocAttrAnimated},
			{Kind: domain.DocAttrVideo, W: 320, H: 240, Duration: 2},
		},
	}
	search := &gifSearchTestSource{docs: []domain.Document{doc}}
	catalog := gifCatalogTestSource{entries: []domain.GifCatalogEntry{{ID: 1, Title: "Local", DocumentID: 5}}}
	svc := NewService(nil, nil, nil, WithGifCatalog(catalog), WithGIFSearch(search))

	got, handled, err := svc.OnInlineQuery(context.Background(), domain.GifBotUserID, 42, "cat", "")
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if search.calls != 1 || len(got.Results) != 1 {
		t.Fatalf("calls=%d results=%d", search.calls, len(got.Results))
	}
	if got.Results[0].ID != "77" || got.Results[0].Title != "cat dance" || got.Results[0].Media.Document.ID != 77 {
		t.Fatalf("result = %+v", got.Results[0])
	}
}

func TestGifBotFallsBackToCatalogWhenSearchIsEmpty(t *testing.T) {
	doc := domain.Document{ID: 9, MimeType: "video/mp4", Attributes: []domain.DocumentAttribute{{Kind: domain.DocAttrAnimated}, {Kind: domain.DocAttrVideo, W: 1, H: 1, Duration: 1}}}
	search := &gifSearchTestSource{}
	svc := NewService(nil, nil, nil,
		WithGifCatalog(gifCatalogTestSource{entries: []domain.GifCatalogEntry{{ID: 1, Title: "Dog", DocumentID: 9}}, docs: []domain.Document{doc}}),
		WithGIFSearch(search))

	got, _, err := svc.OnInlineQuery(context.Background(), domain.GifBotUserID, 42, "dog", "")
	if err != nil || len(got.Results) != 1 || got.Results[0].Media.Document.ID != 9 {
		t.Fatalf("results=%+v err=%v", got.Results, err)
	}
}
