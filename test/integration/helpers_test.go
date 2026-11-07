package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jecklgamis/http-sink/internal/server"
)

// newApp starts a fresh in-process http-sink and returns its base URL.
func newApp(t *testing.T) string {
	t.Helper()
	ts := httptest.NewServer(server.New(server.Options{}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%06x", prefix, rand.IntN(1<<24))
}

type response struct {
	Status int
	Header http.Header
	Raw    []byte
}

// JSON decodes the response body into a generic object.
func (r response) JSON(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Raw, &m); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, r.Raw)
	}
	return m
}

type reqOpt func(*http.Request)

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

// do sends a request; a non-nil body is JSON-encoded.
func do(t *testing.T, method, url string, body any, opts ...reqOpt) response {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return response{Status: res.StatusCode, Header: res.Header, Raw: raw}
}

func wantStatus(t *testing.T, res response, want int) {
	t.Helper()
	if res.Status != want {
		t.Fatalf("status = %d, want %d\nbody: %s", res.Status, want, res.Raw)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
