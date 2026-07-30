package handles

import (
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
