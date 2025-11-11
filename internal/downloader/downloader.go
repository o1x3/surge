package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Downloader struct {
	Client *http.Client //Every downloader has a http client over which the downloads happen
}

func NewDownloader() *Downloader {
	client := http.Client{
		Timeout: 0,
	}
	return &Downloader{&client}
}

func (d *Downloader) Download(ctx context.Context, rawurl, outPath string, concurrent int) error {
	if concurrent > 1 {
		return d.concurrentDownload(ctx, rawurl, outPath, concurrent)
	}
	return d.singleDownload(ctx, rawurl, outPath)
}

func (d *Downloader) singleDownload(ctx context.Context, rawurl, outPath string) error {
	parsed, err := url.Parse(rawurl) //Parses the URL into parts
	if err != nil {
		return err
	}

	if parsed.Scheme == "" {
		return errors.New("url missing scheme (use http:// or https://)")
	} //if the URL does not have a scheme, return an error

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil) //We use a context so that we can cancel the download whenever we want
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) "+
		"Chrome/120.0.0.0 Safari/537.36") // We set a browser like header to avoid being blocked by some websites

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("bad status code: %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode != 200 {
			return fmt.Errorf("bad status code: %d", resp.StatusCode)
		}
	}

	filename := filepath.Base(outPath)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		// naive parsing: look for filename="..."
		if idx := strings.Index(cd, "filename="); idx != -1 {
			name := cd[idx+len("filename="):]
			name = strings.Trim(name, `"' `)
			if name != "" {
				filename = filepath.Base(name)
			}
		}
	}

	outDir := filepath.Dir(outPath)
	tmpFile, err := os.CreateTemp(outDir, filename+".part.*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	defer func() {
		tmpFile.Close()
		// if download failed, remove temp file
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	var total int64 = -1
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if v, e := strconv.ParseInt(cl, 10, 64); e == nil {
			total = v
		}
	}

	// copy loop with manual buffering so we can measure progress
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64 = 0
	lastReport := time.Now()
	start := time.Now()

	for {
		// respect context cancellation: check before read
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			wn, werr := tmpFile.Write(buf[:n])
			if werr != nil {
				return werr
			}
			if wn != n {
				return io.ErrShortWrite
			}
			written += int64(n)
		}

		// progress reporting periodically (every 200ms or on finish)
		now := time.Now()
		if now.Sub(lastReport) > 200*time.Millisecond || readErr == io.EOF {
			printProgress(written, total, start)
			lastReport = now
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}

	// sync file to disk
	if err := tmpFile.Sync(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// atomically move temp to dest
	destPath := outPath
	if info, err := os.Stat(outPath); err == nil && info.IsDir() {
		// When outPath is a directory we must have a valid filename.
		// The filename variable was determined earlier. It might be invalid if derived from a directory name
		if filename == "" || filename == "." || filename == "/" {
			// Try to get it from URL as a last resort
			filename = filepath.Base(parsed.Path)
			if filename == "" || filename == "." || filename == "/" {
				return fmt.Errorf("could not determine filename to save in directory %s", outPath)
			}
		}
		destPath = filepath.Join(outPath, filename)
	}

	if renameErr := os.Rename(tmpPath, destPath); renameErr != nil {
		// fallback: copy if rename fails across filesystems
		in, rerr := os.Open(tmpPath)
		if rerr == nil {
			out, werr := os.Create(destPath)
			if werr == nil {
				_, _ = io.Copy(out, in)
				out.Close()
			}
			in.Close()
		}
		os.Remove(tmpPath)
		return fmt.Errorf("rename failed: %v", renameErr)
	}

	fmt.Fprintf(os.Stderr, "\nDownloaded %s\n", destPath)
	return nil
}

func (d *Downloader) concurrentDownload(ctx context.Context, rawurl, outPath string, concurrent int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.Header.Get("Accept-Ranges") != "bytes" {
		fmt.Println("Server does not support concurrent download, falling back to single thread")
		return d.singleDownload(ctx, rawurl, outPath)
	}

	totalSize, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return err
	}

	chunkSize := totalSize / int64(concurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var written int64

	startTime := time.Now()

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == concurrent-1 {
			end = totalSize - 1
		}

		go func(i int, start, end int64) {
			defer wg.Done()
			err := d.downloadChunk(ctx, rawurl, outPath, i, start, end, &mu, &written, totalSize, startTime)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError downloading chunk %d: %v\n", i, err)
			}
		}(i, start, end)
	}

	wg.Wait()

	// Merge files
	destFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	for i := 0; i < concurrent; i++ {
		partFileName := fmt.Sprintf("%s.part%d", outPath, i)
		partFile, err := os.Open(partFileName)
		if err != nil {
			return err
		}
		_, err = io.Copy(destFile, partFile)
		if err != nil {
			partFile.Close()
			return err
		}
		partFile.Close()
		os.Remove(partFileName)
	}

	fmt.Fprintf(os.Stderr, "\nDownloaded %s\n", outPath)
	return nil
}

func (d *Downloader) downloadChunk(ctx context.Context, rawurl, outPath string, index int, start, end int64, mu *sync.Mutex, written *int64, totalSize int64, startTime time.Time) error {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	partFileName := fmt.Sprintf("%s.part%d", outPath, index)
	partFile, err := os.Create(partFileName)
	if err != nil {
		return err
	}
	defer partFile.Close()

	buf := make([]byte, 32*1024)
	for {

		n, err := resp.Body.Read(buf)

		if n > 0 {
			_, wErr := partFile.Write(buf[:n])
			if wErr != nil {
				return wErr
			}
			mu.Lock()
			*written += int64(n)
			printProgress(*written, totalSize, startTime)
			mu.Unlock()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func printProgress(written, total int64, start time.Time) {
	elapsed := time.Since(start).Seconds()
	speed := float64(written) / 1024.0 / elapsed // KiB/s
	if total > 0 {
		pct := float64(written) / float64(total) * 100.0
		fmt.Fprintf(os.Stderr, "\r%.2f%% %d/%d bytes (%.1f KiB/s)", pct, written, total, speed)
	} else {
		fmt.Fprintf(os.Stderr, "\r%d bytes (%.1f KiB/s)", written, speed)
	}
}
