package application

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/leomaquiaveli/gme-open-server/internal/domain/ports"
)

// downloadCall coordinates goroutines requesting the same URL simultaneously.
// Only the first goroutine fetches; the rest wait and reuse the result.
type downloadCall struct {
	wg   sync.WaitGroup
	path string
	err  error
}

// Downloader deduplicates concurrent downloads of the same URL.
// Wraps IFileCache + IStorage so no use case needs to repeat this logic.
type Downloader struct {
	cache       ports.IFileCache
	storage     ports.IStorage
	workDir     string
	downloading sync.Map // url → *downloadCall
}

func NewDownloader(cache ports.IFileCache, storage ports.IStorage, workDir string) *Downloader {
	return &Downloader{cache: cache, storage: storage, workDir: workDir}
}

// Release decrements the cache refcount. Must be called once per successful Acquire.
func (d *Downloader) Release(url string) {
	d.cache.Release(url)
}

// Invalidate removes url from the cache without touching the file on disk.
// Used after a file is uploaded so the same URL re-used as input forces a fresh download.
func (d *Downloader) Invalidate(url string) {
	d.cache.Invalidate(url)
}

// Acquire returns the local path for url, downloading exactly once even under concurrent load.
// Increments the cache refcount — caller must call Release(url) when done.
func (d *Downloader) Acquire(url string) (string, error) {
	if path, found := d.cache.Get(url); found {
		d.cache.Acquire(url)
		log.Printf("cache hit: %s", url)
		return path, nil
	}

	call := &downloadCall{}
	call.wg.Add(1)
	actual, loaded := d.downloading.LoadOrStore(url, call)
	if loaded {
		// Another goroutine already fetching — wait and reuse.
		existing := actual.(*downloadCall)
		existing.wg.Wait()
		if existing.err != nil {
			return "", existing.err
		}
		d.cache.Acquire(url)
		return existing.path, nil
	}

	log.Printf("cache miss, baixando: %s", url)
	defer func() {
		call.wg.Done()
		d.downloading.Delete(url)
	}()

	destPath := filepath.Join(d.workDir, "cache", cacheKey(url))
	if err := d.storage.Download(url, destPath); err != nil {
		call.err = err
		return "", fmt.Errorf("download %s: %w", url, err)
	}

	d.cache.Put(url, destPath)
	d.cache.Acquire(url)
	call.path = destPath
	return destPath, nil
}
