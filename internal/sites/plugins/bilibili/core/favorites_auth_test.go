//go:build !no_bilibili && (!explicit_sites || site_bilibili)

package bili

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFavFoldersRejectsUnauthenticatedEmptyResponse(t *testing.T) {
	c := NewGuest()
	favoriteRequests := 0
	c.http.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
		data := `{"code":0,"data":{"isLogin":false,"mid":0}}`
		if r.URL.Path == "/x/v3/fav/folder/created/list-all" {
			favoriteRequests++
			data = `{"code":0,"data":{"count":0,"list":[]}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(data)),
			Request:    r,
		}, nil
	})

	folders, err := c.FavFolders(context.Background(), "1808164")
	if !IsAuth(err) {
		t.Fatalf("FavFolders error = %v, want authentication error", err)
	}
	if len(folders) != 0 {
		t.Fatalf("FavFolders returned folders: %+v", folders)
	}
	if favoriteRequests != 0 {
		t.Fatalf("favorite endpoint requests = %d, want 0", favoriteRequests)
	}
}

func TestFavFoldersRejectsDifferentLoggedInAccount(t *testing.T) {
	c := NewGuest()
	c.http.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"data":{"isLogin":true,"mid":42}}`)),
			Request:    r,
		}, nil
	})

	_, err := c.FavFolders(context.Background(), "1808164")
	if !IsAuth(err) {
		t.Fatalf("FavFolders error = %v, want account mismatch authentication error", err)
	}
}

func TestFavFoldersListsFoldersForMatchingLoggedInAccount(t *testing.T) {
	c := NewGuest()
	c.wbi = &wbiKeys{mixinKey: "test"}
	favoriteRequests := 0
	c.http.Transport = identityTransport(func(r *http.Request) (*http.Response, error) {
		data := `{"code":0,"data":{"isLogin":true,"mid":1808164}}`
		if r.URL.Path == "/x/v3/fav/folder/created/list-all" {
			favoriteRequests++
			data = `{"code":0,"data":{"count":1,"list":[{"id":4418815064,"title":"TEST","media_count":2}]}}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(data)),
			Request:    r,
		}, nil
	})

	folders, err := c.FavFolders(context.Background(), "1808164")
	if err != nil {
		t.Fatal(err)
	}
	if favoriteRequests != 1 {
		t.Fatalf("favorite endpoint requests = %d, want 1", favoriteRequests)
	}
	if len(folders) != 1 || folders[0].RemoteID != "4418815064" || folders[0].Count != 2 {
		t.Fatalf("FavFolders = %+v", folders)
	}
}
