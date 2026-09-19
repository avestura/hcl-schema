package hclschema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2"
)

// Loader resolves schema references: local paths, entries in an fs.FS, and
// https URLs.
type Loader struct {
	// Cache stores fetched remote schemas. When nil, DiskCache is used.
	Cache Cache

	// HTTPClient fetches remote schemas. When nil, a client with the redirect
	// policy from SafeRedirectPolicy and Timeout is used.
	HTTPClient *http.Client

	// FS, when set, is consulted for relative references instead of the OS
	// filesystem.
	FS fs.FS

	// Offline forbids network access. Cached copies are still served, with a
	// warning when they are past their TTL.
	Offline bool

	// MaxSize caps the size of a fetched schema. Zero means DefaultMaxSchemaSize.
	MaxSize int64

	// TTL is how long a cached copy is served without revalidating. Zero means
	// DefaultSchemaTTL.
	TTL time.Duration

	// Timeout bounds a single fetch. Zero means DefaultFetchTimeout.
	Timeout time.Duration

	// AllowCrossHostRedirect permits a redirect to a different host. A redirect
	// that downgrades https to http is always refused.
	AllowCrossHostRedirect bool
}

const (
	// DefaultMaxSchemaSize caps a fetched remote schema at 1 MiB.
	DefaultMaxSchemaSize = 1 << 20
	// DefaultSchemaTTL is how long a cached remote schema is reused before
	// being revalidated.
	DefaultSchemaTTL = 24 * time.Hour
	// DefaultFetchTimeout bounds a single remote fetch.
	DefaultFetchTimeout = 15 * time.Second
	// maxRedirects caps a redirect chain.
	maxRedirects = 5
)

// DefaultLoader is used when no loader is supplied.
var DefaultLoader = &Loader{AllowCrossHostRedirect: true}

func (l *Loader) maxSize() int64 {
	if l.MaxSize > 0 {
		return l.MaxSize
	}
	return DefaultMaxSchemaSize
}

func (l *Loader) ttl() time.Duration {
	if l.TTL > 0 {
		return l.TTL
	}
	return DefaultSchemaTTL
}

func (l *Loader) timeout() time.Duration {
	if l.Timeout > 0 {
		return l.Timeout
	}
	return DefaultFetchTimeout
}

func (l *Loader) cache() Cache {
	if l.Cache != nil {
		return l.Cache
	}
	return defaultDiskCache()
}

func (l *Loader) client() *http.Client {
	if l.HTTPClient != nil {
		return l.HTTPClient
	}
	return &http.Client{
		Timeout:       l.timeout(),
		CheckRedirect: l.checkRedirect,
	}
}

// checkRedirect refuses an https-to-http downgrade outright, and a cross-host
// hop unless the loader opts into it. Without this a schema URL on a host you
// trust can silently hand off to one you do not.
func (l *Loader) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	prev := via[len(via)-1]
	if prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect from https to %s", req.URL.Scheme)
	}
	if !l.AllowCrossHostRedirect && req.URL.Host != prev.URL.Host {
		return fmt.Errorf("refusing cross-host redirect to %s", req.URL.Host)
	}
	return nil
}

// IsRemote reports whether a reference names a URL rather than a path.
func IsRemote(ref string) bool {
	return strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://")
}

// dirOf returns the directory part of a reference, handling both URLs and
// filesystem paths.
func dirOf(name string) string {
	if IsRemote(name) {
		if u, err := url.Parse(name); err == nil {
			u.Path = path.Dir(u.Path)
			u.RawQuery = ""
			u.Fragment = ""
			return u.String()
		}
	}
	return filepath.Dir(name)
}

// Resolve turns a reference into an absolute path or URL, relative to base.
func (l *Loader) Resolve(ref, base string) string {
	if IsRemote(ref) {
		return ref
	}
	if base == "" {
		return ref
	}
	if IsRemote(base) {
		if u, err := url.Parse(base); err == nil {
			u.Path = path.Join(u.Path, ref)
			return u.String()
		}
		return ref
	}
	if l.FS != nil {
		return path.Join(base, ref)
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(base, ref)
}

// canonicalKey produces a stable identity for a reference, used for cycle
// detection and cache lookups.
func (l *Loader) canonicalKey(ref, base string) string {
	resolved := l.Resolve(ref, base)
	if IsRemote(resolved) {
		return resolved
	}
	if abs, err := filepath.Abs(resolved); err == nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(resolved)
}

// Load reads a schema reference. wantSum, when non-empty, is a hex-encoded
// SHA-256 digest the content must match. It returns the bytes and the name the
// content should be reported under in diagnostics.
func (l *Loader) Load(ref, base, wantSum string) ([]byte, string, hcl.Diagnostics) {
	resolved := l.Resolve(ref, base)

	if strings.HasPrefix(resolved, "http://") {
		return nil, resolved, hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "Insecure schema URL",
			Detail:   "Only https:// URLs may be used for remote schemas.",
		}}
	}

	if strings.HasPrefix(resolved, "https://") {
		data, diags := l.loadRemote(resolved, wantSum)
		return data, resolved, diags
	}

	var (
		data []byte
		err  error
	)
	if l.FS != nil {
		data, err = fs.ReadFile(l.FS, filepath.ToSlash(resolved))
	} else {
		data, err = os.ReadFile(resolved)
	}
	if err != nil {
		return nil, resolved, hcl.Diagnostics{{
			Severity: hcl.DiagError,
			Summary:  "Failed to read file",
			Detail:   fmt.Sprintf("The schema file %q could not be read: %s.", resolved, err),
		}}
	}
	if d := verifySum(resolved, data, wantSum); d != nil {
		return nil, resolved, hcl.Diagnostics{d}
	}
	return data, resolved, nil
}

func (l *Loader) loadRemote(rawURL, wantSum string) ([]byte, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	c := l.cache()
	entry, cached := c.Get(rawURL)

	if cached {
		if d := verifySum(rawURL, entry.Data, wantSum); d != nil {
			// A cached copy that fails its pin is evidence the cache is stale
			// or tampered with, so drop it rather than serving it.
			_ = c.Delete(rawURL)
			cached = false
		} else if time.Since(entry.FetchedAt) < l.ttl() {
			return entry.Data, diags
		}
	}

	if l.Offline {
		if cached {
			return entry.Data, append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Serving a stale schema",
				Detail:   fmt.Sprintf("Offline mode is on and the cached copy of %s is past its TTL.", rawURL),
			})
		}
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Schema unavailable offline",
			Detail:   fmt.Sprintf("Offline mode is on and %s is not in the cache.", rawURL),
		})
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to create request",
			Detail:   err.Error(),
		})
	}
	req.Header.Set("Accept", "text/plain, application/hcl, */*")
	if cached && entry.ETag != "" {
		req.Header.Set("If-None-Match", entry.ETag)
	}

	resp, err := l.client().Do(req)
	if err != nil {
		if cached {
			return entry.Data, append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Serving a stale schema",
				Detail:   fmt.Sprintf("Could not refresh %s (%s); using the cached copy.", rawURL, err),
			})
		}
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to download schema",
			Detail:   err.Error(),
		})
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && cached {
		entry.FetchedAt = time.Now()
		_ = c.Put(rawURL, entry)
		return entry.Data, diags
	}
	if resp.StatusCode != http.StatusOK {
		if cached {
			return entry.Data, append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Serving a stale schema",
				Detail:   fmt.Sprintf("%s returned %s; using the cached copy.", rawURL, resp.Status),
			})
		}
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to download schema",
			Detail:   fmt.Sprintf("%s returned %s.", rawURL, resp.Status),
		})
	}

	max := l.maxSize()
	data, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Failed to read schema body",
			Detail:   err.Error(),
		})
	}
	if int64(len(data)) > max {
		return nil, append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Schema too large",
			Detail:   fmt.Sprintf("%s exceeds the %d byte limit.", rawURL, max),
		})
	}
	if d := verifySum(rawURL, data, wantSum); d != nil {
		return nil, append(diags, d)
	}

	_ = c.Put(rawURL, &CacheEntry{
		Data:      data,
		ETag:      resp.Header.Get("ETag"),
		FetchedAt: time.Now(),
	})
	return data, diags
}

// Sum returns the hex-encoded SHA-256 digest of schema content, in the form
// accepted by `__schema_sha256` and by an import's `sha256`.
func Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func verifySum(name string, data []byte, want string) *hcl.Diagnostic {
	if want == "" {
		return nil
	}
	got := Sum(data)
	if !strings.EqualFold(got, want) {
		return &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "Schema checksum mismatch",
			Detail:   fmt.Sprintf("%s has digest %s, but %s was expected.", name, got, want),
		}
	}
	return nil
}

// CacheEntry is one cached remote schema.
type CacheEntry struct {
	Data      []byte    `json:"-"`
	ETag      string    `json:"etag"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Cache stores fetched remote schemas.
type Cache interface {
	Get(key string) (*CacheEntry, bool)
	Put(key string, entry *CacheEntry) error
	Delete(key string) error
}

// DiskCache stores schemas under a directory. Unlike the shared temp directory
// used before, the default location is the per-user cache directory with
// owner-only permissions, so another account on the machine cannot plant a
// schema that this process will then trust.
type DiskCache struct {
	Dir string
}

func defaultDiskCache() Cache {
	return &DiskCache{Dir: DefaultCacheDir()}
}

// DefaultCacheDir is where remote schemas are cached.
func DefaultCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "hclschema")
	}
	return filepath.Join(os.TempDir(), "hclschema-cache")
}

func (c *DiskCache) paths(key string) (data string, meta string) {
	h := sha256.Sum256([]byte(key))
	base := filepath.Join(c.Dir, hex.EncodeToString(h[:]))
	return base + ".schema.hcl", base + ".json"
}

// Get returns a cached entry.
func (c *DiskCache) Get(key string) (*CacheEntry, bool) {
	dataPath, metaPath := c.paths(key)
	data, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, false
	}
	entry := &CacheEntry{Data: data}
	if raw, err := os.ReadFile(metaPath); err == nil {
		_ = json.Unmarshal(raw, entry)
	}
	if entry.FetchedAt.IsZero() {
		if fi, err := os.Stat(dataPath); err == nil {
			entry.FetchedAt = fi.ModTime()
		}
	}
	return entry, true
}

// Put stores an entry.
func (c *DiskCache) Put(key string, entry *CacheEntry) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	dataPath, metaPath := c.paths(key)
	if err := writeFilePrivate(dataPath, entry.Data); err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return writeFilePrivate(metaPath, raw)
}

// Delete removes an entry.
func (c *DiskCache) Delete(key string) error {
	dataPath, metaPath := c.paths(key)
	err := os.Remove(dataPath)
	if err2 := os.Remove(metaPath); err == nil && !errors.Is(err2, os.ErrNotExist) {
		err = err2
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// writeFilePrivate writes via a temporary file in the same directory so that a
// reader never observes a half-written schema.
func writeFilePrivate(dst string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, os.ErrInvalid) {
		// Chmod is a no-op on some platforms; a failure here is not fatal.
		_ = err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// MemoryCache is an in-process cache, useful in tests and in long-running
// processes such as the language server.
type MemoryCache struct {
	entries map[string]*CacheEntry
}

// NewMemoryCache returns an empty in-process cache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{entries: map[string]*CacheEntry{}}
}

// Get returns a cached entry.
func (c *MemoryCache) Get(key string) (*CacheEntry, bool) {
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	clone := *e
	return &clone, true
}

// Put stores an entry.
func (c *MemoryCache) Put(key string, entry *CacheEntry) error {
	if c.entries == nil {
		c.entries = map[string]*CacheEntry{}
	}
	clone := *entry
	c.entries[key] = &clone
	return nil
}

// Delete removes an entry.
func (c *MemoryCache) Delete(key string) error {
	delete(c.entries, key)
	return nil
}
