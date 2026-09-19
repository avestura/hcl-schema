package hclschema

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func metaSource(t *testing.T) []byte {
	t.Helper()
	return mustRead(t, filepath.Join("..", "..", "schema", "draft", "2026-09", ".schema.hcl"))
}

func TestLoadRemoteSchemaCaches(t *testing.T) {
	body := metaSource(t)
	var hits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("ETag", `"v1"`)
		w.Write(body)
	}))
	defer srv.Close()

	l := &Loader{
		Cache:                  NewMemoryCache(),
		HTTPClient:             srv.Client(),
		AllowCrossHostRedirect: true,
	}
	url := srv.URL + "/.schema.hcl"

	for i := 0; i < 3; i++ {
		data, name, diags := l.Load(url, "", "")
		if diags.HasErrors() {
			t.Fatalf("load %d: %v", i, diags)
		}
		if name != url {
			t.Fatalf("name = %q, want the URL", name)
		}
		if len(data) != len(body) {
			t.Fatalf("length = %d, want %d", len(data), len(body))
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server was hit %d times; the cache should have served the rest", got)
	}
}

// A past-TTL entry is revalidated, and a 304 refreshes it without a re-download.
func TestLoadRemoteRevalidatesWithETag(t *testing.T) {
	body := metaSource(t)
	var full, notModified int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			atomic.AddInt32(&notModified, 1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		atomic.AddInt32(&full, 1)
		w.Header().Set("ETag", `"v1"`)
		w.Write(body)
	}))
	defer srv.Close()

	l := &Loader{
		Cache:                  NewMemoryCache(),
		HTTPClient:             srv.Client(),
		TTL:                    time.Nanosecond,
		AllowCrossHostRedirect: true,
	}
	url := srv.URL + "/.schema.hcl"

	if _, _, d := l.Load(url, "", ""); d.HasErrors() {
		t.Fatalf("first load: %v", d)
	}
	time.Sleep(2 * time.Millisecond)
	data, _, d := l.Load(url, "", "")
	if d.HasErrors() {
		t.Fatalf("second load: %v", d)
	}
	if len(data) != len(body) {
		t.Fatal("revalidated load returned the wrong body")
	}
	if full != 1 || notModified != 1 {
		t.Fatalf("full=%d notModified=%d, want 1 and 1", full, notModified)
	}
}

func TestRemoteSchemaDigestPinning(t *testing.T) {
	body := metaSource(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	newLoader := func() *Loader {
		return &Loader{
			Cache:                  NewMemoryCache(),
			HTTPClient:             srv.Client(),
			AllowCrossHostRedirect: true,
		}
	}
	url := srv.URL + "/.schema.hcl"

	if _, _, d := newLoader().Load(url, "", Sum(body)); d.HasErrors() {
		t.Fatalf("matching digest should load: %v", d)
	}

	wrong := strings.Repeat("0", 64)
	_, _, d := newLoader().Load(url, "", wrong)
	assertErrorContaining(t, d, "checksum mismatch")
}

func TestInsecureURLRejected(t *testing.T) {
	_, _, d := DefaultLoader.Load("http://example.com/x.schema.hcl", "", "")
	assertErrorContaining(t, d, "Insecure schema URL")
}

// A redirect that drops from https to http would hand the schema to an
// attacker on the path, so it is refused even though the first hop was secure.
func TestRedirectDowngradeRefused(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body {}\n"))
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x.schema.hcl", http.StatusFound)
	}))
	defer secure.Close()

	l := &Loader{
		Cache:                  NewMemoryCache(),
		HTTPClient:             &http.Client{Transport: secure.Client().Transport},
		AllowCrossHostRedirect: true,
	}
	l.HTTPClient.CheckRedirect = l.checkRedirect

	_, _, d := l.Load(secure.URL+"/.schema.hcl", "", "")
	assertErrorContaining(t, d, "Failed to download schema")
}

func TestCrossHostRedirectRefusedWhenDisallowed(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("body {}\n"))
	}))
	defer target.Close()

	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/x.schema.hcl", http.StatusFound)
	}))
	defer origin.Close()

	l := &Loader{Cache: NewMemoryCache(), AllowCrossHostRedirect: false}
	l.HTTPClient = &http.Client{
		Transport:     origin.Client().Transport,
		CheckRedirect: l.checkRedirect,
	}

	_, _, d := l.Load(origin.URL+"/.schema.hcl", "", "")
	assertErrorContaining(t, d, "Failed to download schema")
}

func TestSchemaSizeLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()

	l := &Loader{
		Cache:                  NewMemoryCache(),
		HTTPClient:             srv.Client(),
		MaxSize:                1024,
		AllowCrossHostRedirect: true,
	}
	_, _, d := l.Load(srv.URL+"/.schema.hcl", "", "")
	assertErrorContaining(t, d, "Schema too large")
}

func TestOfflineWithoutCacheIsAnError(t *testing.T) {
	l := &Loader{Cache: NewMemoryCache(), Offline: true}
	_, _, d := l.Load("https://example.invalid/x.schema.hcl", "", "")
	assertErrorContaining(t, d, "unavailable offline")
}

func TestOfflineServesCachedCopyWithWarning(t *testing.T) {
	cache := NewMemoryCache()
	url := "https://example.invalid/x.schema.hcl"
	if err := cache.Put(url, &CacheEntry{
		Data:      []byte("body {}\n"),
		FetchedAt: time.Now().Add(-48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	l := &Loader{Cache: cache, Offline: true}
	data, _, d := l.Load(url, "", "")
	if d.HasErrors() {
		t.Fatalf("expected a warning, not an error: %v", d)
	}
	if len(d) != 1 || d[0].Summary != "Serving a stale schema" {
		t.Fatalf("expected a staleness warning, got %v", d)
	}
	if string(data) != "body {}\n" {
		t.Fatalf("unexpected body %q", data)
	}
}

// A cached copy that no longer matches its pin is evidence of tampering or a
// stale cache, so it must be dropped rather than served.
func TestCachedCopyFailingItsPinIsDiscarded(t *testing.T) {
	body := []byte("body {}\n")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	cache := NewMemoryCache()
	url := srv.URL + "/x.schema.hcl"
	if err := cache.Put(url, &CacheEntry{
		Data:      []byte("body { evil = true }\n"),
		FetchedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	l := &Loader{
		Cache:                  cache,
		HTTPClient:             srv.Client(),
		AllowCrossHostRedirect: true,
	}
	data, _, d := l.Load(url, "", Sum(body))
	if d.HasErrors() {
		t.Fatalf("expected the poisoned entry to be refetched: %v", d)
	}
	if string(data) != string(body) {
		t.Fatalf("served the poisoned copy: %q", data)
	}
}

func TestDiskCacheUsesPrivatePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	c := &DiskCache{Dir: dir}
	if err := c.Put("k", &CacheEntry{Data: []byte("x"), FetchedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Permission bits are not meaningful on Windows, so only the POSIX case is
	// asserted; the call above still has to succeed everywhere.
	if os.PathSeparator == '/' && info.Mode().Perm() != 0o700 {
		t.Fatalf("cache dir mode = %v, want 0700", info.Mode().Perm())
	}

	got, ok := c.Get("k")
	if !ok || string(got.Data) != "x" {
		t.Fatalf("round trip failed: %v %v", ok, got)
	}
	if err := c.Delete("k"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get("k"); ok {
		t.Fatal("entry survived deletion")
	}
}

func TestDefaultCacheDirIsPerUser(t *testing.T) {
	dir := DefaultCacheDir()
	if dir == "" {
		t.Fatal("empty cache dir")
	}
	if strings.Contains(strings.ToLower(dir), "hclschema-cache") && strings.HasPrefix(dir, os.TempDir()) {
		// Only acceptable as the fallback when the user cache dir is unknown.
		if _, err := os.UserCacheDir(); err == nil {
			t.Fatalf("cache dir fell back to the shared temp dir: %s", dir)
		}
	}
}

func TestResolveRelativeToRemoteBase(t *testing.T) {
	l := &Loader{}
	got := l.Resolve("./common.schema.hcl", "https://example.com/schemas")
	want := "https://example.com/schemas/common.schema.hcl"
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestDirOfHandlesURLs(t *testing.T) {
	if got := dirOf("https://example.com/a/b/.schema.hcl"); got != "https://example.com/a/b" {
		t.Fatalf("dirOf = %q", got)
	}
}

func TestSumIsStable(t *testing.T) {
	if Sum([]byte("abc")) != fmt.Sprintf("%x", [32]byte{
		0xba, 0x78, 0x16, 0xbf, 0x8f, 0x01, 0xcf, 0xea,
		0x41, 0x41, 0x40, 0xde, 0x5d, 0xae, 0x22, 0x23,
		0xb0, 0x03, 0x61, 0xa3, 0x96, 0x17, 0x7a, 0x9c,
		0xb4, 0x10, 0xff, 0x61, 0xf2, 0x00, 0x15, 0xad,
	}) {
		t.Fatal("Sum does not match the known SHA-256 of \"abc\"")
	}
}
