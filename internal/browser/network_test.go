package browser

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetcherReadsLocalPathAndFileURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "page with space.html")
	want := []byte("<p>local</p>")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	fileURL := (&url.URL{Scheme: "file", Path: path}).String()

	for _, source := range []string{path, fileURL} {
		resource, err := (&Fetcher{}).Fetch(source)
		if err != nil {
			t.Fatalf("Fetch(%q): %v", source, err)
		}
		if string(resource.Body) != string(want) {
			t.Errorf("Fetch(%q) body = %q, want %q", source, resource.Body, want)
		}
		if resource.Source != source {
			t.Errorf("Fetch(%q) source = %q", source, resource.Source)
		}
	}
}

func TestFetcherFollowsRelativeRedirectAndReadsContentLength(t *testing.T) {
	var dialCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Proto != "HTTP/1.1" || r.Method != http.MethodGet {
			t.Errorf("request = %s %s, want HTTP/1.1 GET", r.Proto, r.Method)
		}
		switch r.URL.Path {
		case "/dir/start":
			http.Redirect(w, r, "../content?query=yes", http.StatusFound)
		case "/content":
			if r.URL.Query().Get("query") != "yes" {
				t.Errorf("query = %q", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, "length-delimited")
		default:
			t.Errorf("unexpected request path %q, raw query %q", r.URL.Path, r.URL.RawQuery)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := &Fetcher{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialCount.Add(1)
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	}
	resource, err := fetcher.Fetch(server.URL + "/dir/start")
	if err != nil {
		t.Fatal(err)
	}
	if string(resource.Body) != "length-delimited" {
		t.Fatalf("body = %q", resource.Body)
	}
	if dialCount.Load() != 2 {
		t.Fatalf("dial count = %d, want redirect and target requests", dialCount.Load())
	}
}

func TestFetcherReadsChunkedAndConnectionCloseBodies(t *testing.T) {
	chunked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("test server does not support flushing")
		}
		_, _ = io.WriteString(w, "first ")
		flusher.Flush()
		_, _ = io.WriteString(w, "second")
		flusher.Flush()
	}))
	defer chunked.Close()
	resource, err := (&Fetcher{}).Fetch(chunked.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(resource.Body) != "first second" {
		t.Fatalf("chunked body = %q", resource.Body)
	}

	closeURL := rawResponseServer(t, "HTTP/1.1 200 OK\r\nConnection: close\r\n\r\nclose-delimited")
	resource, err = (&Fetcher{}).Fetch(closeURL)
	if err != nil {
		t.Fatal(err)
	}
	if string(resource.Body) != "close-delimited" {
		t.Fatalf("connection-close body = %q", resource.Body)
	}
}

func TestFetcherDecodesGzip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Errorf("Accept-Encoding = %q", r.Header.Get("Accept-Encoding"))
		}
		var encoded strings.Builder
		writer := gzip.NewWriter(&encoded)
		_, _ = io.WriteString(writer, "compressed page")
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = io.WriteString(w, encoded.String())
	}))
	defer server.Close()
	resource, err := (&Fetcher{}).Fetch(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(resource.Body) != "compressed page" {
		t.Fatalf("decoded body = %q", resource.Body)
	}
}

func TestFetcherSupportsTLSWithInjectedConfig(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "secure")
	}))
	defer server.Close()
	fetcher := &Fetcher{TLSConfig: &tls.Config{InsecureSkipVerify: true}} // test server uses a private certificate
	resource, err := fetcher.Fetch(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(resource.Body) != "secure" {
		t.Fatalf("body = %q", resource.Body)
	}
}

func TestFetcherEnforcesLimitsAndRejectsMalformedResponses(t *testing.T) {
	t.Run("body limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "123456")
		}))
		defer server.Close()
		_, err := (&Fetcher{MaxBodyBytes: 5}).Fetch(server.URL)
		if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
			t.Fatalf("Fetch() error = %v, want body limit error", err)
		}
	})

	t.Run("header limit", func(t *testing.T) {
		target := rawResponseServer(t, "HTTP/1.1 200 OK\r\nX-Test: too-large\r\n\r\n")
		_, err := (&Fetcher{MaxHeaderBytes: 20}).Fetch(target)
		if err == nil {
			t.Fatal("Fetch() error = nil, want header limit error")
		}
	})

	t.Run("conflicting framing", func(t *testing.T) {
		target := rawResponseServer(t, "HTTP/1.1 200 OK\r\nContent-Length: 1\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n")
		_, err := (&Fetcher{}).Fetch(target)
		if err == nil || !strings.Contains(err.Error(), "both Transfer-Encoding and Content-Length") {
			t.Fatalf("Fetch() error = %v, want conflicting framing error", err)
		}
	})

	t.Run("unsupported encoding", func(t *testing.T) {
		target := rawResponseServer(t, "HTTP/1.1 200 OK\r\nContent-Encoding: br\r\nContent-Length: 0\r\n\r\n")
		_, err := (&Fetcher{}).Fetch(target)
		if err == nil || !strings.Contains(err.Error(), "unsupported Content-Encoding") {
			t.Fatalf("Fetch() error = %v, want unsupported encoding error", err)
		}
	})
}

func TestFetcherBoundsRedirectsAndHonorsTimeout(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	_, err := (&Fetcher{MaxRedirects: 2}).Fetch(redirect.URL + "/loop")
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("Fetch() error = %v, want redirect limit error", err)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = io.WriteString(w, "late")
	}))
	defer slow.Close()
	_, err = (&Fetcher{Timeout: 10 * time.Millisecond}).Fetch(slow.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want timeout")
	}
}

func TestFetcherRejectsInvalidURLs(t *testing.T) {
	for _, source := range []string{"", "ftp://example.test/file", "http://user@example.test/"} {
		if _, err := (&Fetcher{}).Fetch(source); err == nil {
			t.Errorf("Fetch(%q) error = nil, want error", source)
		}
	}
}

func rawResponseServer(t *testing.T, response string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || line == "\r\n" {
				break
			}
		}
		_, _ = fmt.Fprint(conn, response)
	}()
	return "http://" + listener.Addr().String() + "/"
}
