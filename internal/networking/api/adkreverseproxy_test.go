package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewReverseProxy_RoundTrip confirms method, path, query, headers, and
// body all pass through to the backend unchanged, and the backend's
// response (status, header, body) passes back unchanged too.
func TestNewReverseProxy_RoundTrip(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("backend saw method %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/run" {
			t.Errorf("backend saw path %q, want /api/run", r.URL.Path)
		}
		if r.URL.Query().Get("foo") != "bar" {
			t.Errorf("backend saw query %q, want foo=bar", r.URL.RawQuery)
		}
		if got := r.Header.Get("X-Test-Header"); got != "hello" {
			t.Errorf("backend saw X-Test-Header %q, want %q", got, "hello")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "request body" {
			t.Errorf("backend saw body %q, want %q", body, "request body")
		}

		w.Header().Set("X-Backend-Header", "world")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("response body"))
	}))
	defer backend.Close()

	proxy, err := NewReverseProxy(backend.URL, 5*time.Second, nil)
	if err != nil {
		t.Fatalf("NewReverseProxy: %v", err)
	}
	front := httptest.NewServer(proxy)
	defer front.Close()

	req, _ := http.NewRequest(http.MethodPost, front.URL+"/api/run?foo=bar", strings.NewReader("request body"))
	req.Header.Set("X-Test-Header", "hello")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusTeapot)
	}
	if got := resp.Header.Get("X-Backend-Header"); got != "world" {
		t.Errorf("response header X-Backend-Header = %q, want %q", got, "world")
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "response body" {
		t.Errorf("response body = %q, want %q", body, "response body")
	}
}

// TestNewReverseProxy_BackendTimeout confirms a slow backend results in a
// 502 from the proxy rather than the caller hanging indefinitely.
func TestNewReverseProxy_BackendTimeout(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	proxy, err := NewReverseProxy(backend.URL, 20*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("NewReverseProxy: %v", err)
	}
	front := httptest.NewServer(proxy)
	defer front.Close()

	resp, err := http.Get(front.URL + "/api/list-apps")
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want %d (backend should have timed out)", resp.StatusCode, http.StatusBadGateway)
	}
}
