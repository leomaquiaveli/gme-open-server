package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testURL = "https://example.com/video.mp4"

func TestFileCacheGetPutBasic(t *testing.T) {
	c := NewFileCache(time.Minute)

	if _, ok := c.Get(testURL); ok {
		t.Fatal("expected miss on empty cache")
	}

	c.Put(testURL, "/tmp/video.mp4")

	path, ok := c.Get(testURL)
	if !ok {
		t.Fatal("expected hit after Put")
	}
	if path != "/tmp/video.mp4" {
		t.Fatalf("expected /tmp/video.mp4, got %s", path)
	}
}

func TestFileCacheExpiry(t *testing.T) {
	c := NewFileCache(50 * time.Millisecond)
	c.Put(testURL, "/tmp/video.mp4")

	time.Sleep(100 * time.Millisecond)

	if _, ok := c.Get(testURL); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestFileCacheRefCount(t *testing.T) {
	c := NewFileCache(time.Minute)
	c.Put(testURL, "/tmp/video.mp4")

	c.Acquire(testURL)
	c.Acquire(testURL)

	c.Release(testURL) // refCount=1
	if _, ok := c.Get(testURL); !ok {
		t.Fatal("entry should still exist after partial release")
	}

	c.Release(testURL) // refCount=0
	if _, ok := c.Get(testURL); !ok {
		t.Fatal("entry with refCount=0 and live TTL should remain in cache")
	}
}

func TestSweepOnceRemovesOrphanFile(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "orphan.mp4")

	if err := os.WriteFile(filePath, []byte("fake video"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	c := NewFileCache(50 * time.Millisecond)
	c.Put(testURL, filePath)
	c.Acquire(testURL)
	c.Release(testURL) // refCount back to 0

	// TTL has not expired yet — sweepOnce must NOT delete
	c.sweepOnce()
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("sweepOnce deleted file before TTL expired")
	}

	time.Sleep(100 * time.Millisecond) // wait for TTL to expire

	c.sweepOnce()

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("sweepOnce did not delete orphan file after TTL expired")
	}
	if _, ok := c.Get(testURL); ok {
		t.Fatal("sweepOnce did not remove expired entry from map")
	}
}

func TestSweepOncePreservesActiveEntry(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "active.mp4")

	if err := os.WriteFile(filePath, []byte("fake video"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	c := NewFileCache(50 * time.Millisecond)
	c.Put(testURL, filePath)
	c.Acquire(testURL) // refCount=1, job is using the file

	time.Sleep(100 * time.Millisecond) // TTL expired

	c.sweepOnce() // refCount > 0, must NOT delete

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("sweepOnce deleted file that is still in use (refCount > 0)")
	}

	c.Release(testURL) // refCount=0
	c.sweepOnce()      // now it should delete

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("sweepOnce did not delete file after last Release + TTL expired")
	}
}

func TestSweepInterval(t *testing.T) {
	cases := []struct {
		ttl      time.Duration
		wantMin  time.Duration
		wantMax  time.Duration
	}{
		{10 * time.Millisecond, time.Second, time.Second},         // floor
		{2 * time.Minute, time.Minute, time.Minute},
		{60 * time.Minute, 30 * time.Minute, 30 * time.Minute},   // ceil
		{120 * time.Minute, 30 * time.Minute, 30 * time.Minute},  // above ceil
	}
	for _, tc := range cases {
		got := sweepInterval(tc.ttl)
		if got < tc.wantMin || got > tc.wantMax {
			t.Errorf("sweepInterval(%v) = %v, want [%v, %v]", tc.ttl, got, tc.wantMin, tc.wantMax)
		}
	}
}
