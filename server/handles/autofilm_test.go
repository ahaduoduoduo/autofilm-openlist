package handles

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/gin-gonic/gin"
)

func TestAutoFilmVirtualListResponse(t *testing.T) {
	response := autoFilmVirtualListResponse("/", []model.Obj{
		&model.Object{
			Name:     "media",
			IsFolder: true,
		},
	})

	if len(response.Objects) != 1 {
		t.Fatalf("expected one object, got %d", len(response.Objects))
	}
	object := response.Objects[0]
	if object.Path != "/media" || object.Name != "media" || !object.IsDir {
		t.Fatalf("unexpected virtual object: %+v", object)
	}
}

func TestFindAutoFilmObjectUsesRefreshedDirectoryEntry(t *testing.T) {
	objects := []model.Obj{
		&model.Object{Name: "existing.mkv"},
		&model.Object{Name: "completed-download", IsFolder: true},
	}

	found := findAutoFilmObject(objects, "completed-download")
	if found == nil || found.GetName() != "completed-download" {
		t.Fatalf("unexpected object: %+v", found)
	}
	if missing := findAutoFilmObject(objects, "missing"); missing != nil {
		t.Fatalf("expected missing object, got %+v", missing)
	}
}

func TestNormalizeAutoFilmJellyfinPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "file", value: "/115/movie/title.mkv", want: "/115/movie/title.mkv"},
		{name: "clean", value: " /115//movie/ ", want: "/115/movie"},
		{name: "relative", value: "115/movie", wantErr: true},
		{name: "root", value: "/", wantErr: true},
		{name: "parent traversal", value: "/115/../secret", wantErr: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeAutoFilmJellyfinPath(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize path: %v", err)
			}
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeAutoFilmJellyfinScanMode(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value   string
		want    string
		wantErr bool
	}{
		{value: "", want: "new"},
		{value: " NEW ", want: "new"},
		{value: "full", want: "full"},
		{value: "replace", wantErr: true},
	} {
		got, err := normalizeAutoFilmJellyfinScanMode(test.value)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected %q to fail, got %q", test.value, got)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("mode %q: got (%q, %v), want %q", test.value, got, err, test.want)
		}
	}
}

func TestRequestAutoFilmJellyfinRefresh(t *testing.T) {
	var receivedAuthorization string
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		receivedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"action":"created","path":"openlist:///115/movie/title.mkv"}`))
	}))
	defer server.Close()

	t.Setenv(autoFilmJellyfinURLEnvironment, server.URL)
	t.Setenv(autoFilmJellyfinAPIKeyEnvironment, "test-api-key")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	result, err := requestAutoFilmJellyfinRefresh(
		context,
		"/115/movie/title.mkv",
		AutoFilmJellyfinScanReq{
			Refresh:    true,
			Recursive:  false,
			ForceProbe: true,
			ScanMode:   "full",
		},
	)
	if err != nil {
		t.Fatalf("request refresh: %v", err)
	}
	if !strings.Contains(receivedAuthorization, `Token="test-api-key"`) {
		t.Fatalf("unexpected authorization %q", receivedAuthorization)
	}
	for _, expected := range []string{
		`"path":"/115/movie/title.mkv"`,
		`"refresh":true`,
		`"recursive":false`,
		`"force_probe":true`,
		`"scan_mode":"full"`,
	} {
		if !strings.Contains(receivedBody, expected) {
			t.Fatalf("request body %q does not contain %q", receivedBody, expected)
		}
	}
	response, ok := result.(map[string]any)
	if !ok || response["action"] != "created" {
		t.Fatalf("unexpected response: %#v", result)
	}
}

func TestOpenListPathFromJellyfinLocation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		want  string
		ok    bool
	}{
		{value: "openlist:///115/movie", want: "/115/movie", ok: true},
		{value: "openlist://115/movie", want: "/115/movie", ok: true},
		{value: "/movie", ok: false},
	}
	for _, test := range tests {
		got, ok := openListPathFromJellyfinLocation(test.value)
		if ok != test.ok || got != test.want {
			t.Fatalf(
				"location %q: got (%q, %v), want (%q, %v)",
				test.value,
				got,
				ok,
				test.want,
				test.ok,
			)
		}
	}
}

func TestRequestAutoFilmJellyfinPathStatus(t *testing.T) {
	var receivedAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"Name":"Movies","Locations":["openlist:///115/movie"]},
			{"Name":"Local","Locations":["/movie"]}
		]`))
	}))
	defer server.Close()

	t.Setenv(autoFilmJellyfinURLEnvironment, server.URL)
	t.Setenv(autoFilmJellyfinAPIKeyEnvironment, "test-api-key")
	status, err := requestAutoFilmJellyfinPathStatus(
		context.Background(),
		"/115/movie/title",
	)
	if err != nil {
		t.Fatalf("request path status: %v", err)
	}
	if !status.Configured ||
		status.LibraryName != "Movies" ||
		status.MatchingRoot != "/115/movie" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if !strings.Contains(receivedAuthorization, `Token="test-api-key"`) {
		t.Fatalf("unexpected authorization %q", receivedAuthorization)
	}

	status, err = requestAutoFilmJellyfinPathStatus(
		context.Background(),
		"/115/not-configured",
	)
	if err != nil {
		t.Fatalf("request unconfigured path status: %v", err)
	}
	if status.Configured || status.Message == "" {
		t.Fatalf("unexpected unconfigured status: %+v", status)
	}
}
