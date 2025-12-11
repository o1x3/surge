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
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"surge/internal/utils"

	"github.com/vfaronov/httpheader"

	"surge/internal/messages"
)

type Downloader struct {
	Client                   *http.Client //Every downloader has a http client over which the downloads happen
	bytesDownloadedPerSecond []int64
	mu                       sync.Mutex
	ProgressChan             chan<- tea.Msg // Use tea.Msg generic channel to send different message types
	ID                       int
}

func (d *Downloader) SetProgressChan(ch chan<- tea.Msg) {
	d.ProgressChan = ch
}

func (d *Downloader) SetID(id int) {
	d.ID = id
}

func NewDownloader() *Downloader {
	client := http.Client{
		Timeout: 0,
	}
	return &Downloader{Client: &client}
}

func (d *Downloader) Download(ctx context.Context, rawurl, outPath string, concurrent int, verbose bool, md5sum, sha256sum string) error {
	if concurrent > 1 {
		return d.concurrentDownload(ctx, rawurl, outPath, verbose, md5sum, sha256sum)
	}
	return d.singleDownload(ctx, rawurl, outPath, verbose)
}

func (d *Downloader) singleDownload(ctx context.Context, rawurl, outPath string, verbose bool) (err error) {
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

	resp, err := d.Client.Do(req) //Exectes the HTTP request
	if err != nil {
		return err
	}
	defer resp.Body.Close() //Closes the response body when the function returns

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	filename := filepath.Base(outPath)

	// Try to extract filename from Content-Disposition header
	if _, name, err := httpheader.ContentDisposition(resp.Header); err == nil && name != "" {
		filename = filepath.Base(name)
	}

	// Fallback: Only use download.bin if filename is still invalid
	if filename == "" || filename == "." || filename == "/" {
		filename = "download.bin"
	}

	var totalSize int64 = -1
	contentLength := resp.Header.Get("Content-Length")
	if contentLength != "" {
		totalSize, _ = strconv.ParseInt(contentLength, 10, 64)
	}

	// Send DownloadStartedMsg
	if d.ProgressChan != nil {
		d.ProgressChan <- messages.DownloadStartedMsg{
			DownloadID: d.ID,
			URL:        rawurl,
			Filename:   filename,
			Total:      totalSize,
		}
	}

	outDir := filepath.Dir(outPath)
	tmpFile, err := os.CreateTemp(outDir, filename+".part.*") //Tries to create a temporary file
	if err != nil {
		return err
	} // Returns error if it fails to create temp file
	tmpPath := tmpFile.Name()
	closed := false

	defer func() {
		if !closed {
			tmpFile.Close()
		}
		// if download failed, remove temp file
		if err != nil {
			os.Remove(tmpPath)
		}
	}() //Waits until the function returns and closes the temp file and removes it if there was an error

	start := time.Now()

	// Copy response body to file efficiently
	var written int64
	buf := make([]byte, 32*1024)
	lastUpdate := time.Now()

	for {
		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			nw, ew := tmpFile.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}

			// Update progress every 100ms
			if time.Since(lastUpdate) > 100*time.Millisecond {
				d.printProgress(written, totalSize, start, verbose, 1)
				lastUpdate = time.Now()
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	// Final update
	d.printProgress(written, totalSize, start, verbose, 1)

	if err != nil {
		return fmt.Errorf("copy failed: %w", err)
	}
	// Sync file to disk
	if err = tmpFile.Sync(); err != nil {
		return err
	}
	if err = tmpFile.Close(); err != nil {
		return err
	}
	closed = true

	elapsed := time.Since(start)
	speed := float64(written) / 1024.0 / elapsed.Seconds() // KiB/s
	fmt.Fprintf(os.Stderr, "\nDownloaded %s in %s (%s/s)\n",
		outPath,
		elapsed.Round(time.Second),
		utils.ConvertBytesToHumanReadable(int64(speed*1024)),
	)

	if d.ProgressChan != nil {
		d.ProgressChan <- messages.DownloadCompleteMsg{
			DownloadID: d.ID,
			Filename:   filename,
			Elapsed:    elapsed,
			Total:      written,
		}
	}

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

	if renameErr := os.Rename(tmpPath, destPath); renameErr != nil { //If renaming fails, we do a manual copy
		if in, rerr := os.Open(tmpPath); rerr == nil { // Opens temp file for reading
			defer in.Close()                                   //Waits until function returns to close temp file
			if out, werr := os.Create(destPath); werr == nil { //Creates destination file
				defer out.Close()                             //Waits until function returns to close destination file
				if _, cerr := io.Copy(out, in); cerr != nil { //Tries to copy from temp to destination
					return cerr // return the real copy error
				}
			} else {
				return werr // handle file creation error
			}
		} else {
			return rerr // handle file open error
		}
		os.Remove(tmpPath) //only remove after successful copy
		return nil
	}

	return nil
}

// func (d *Downloader) concurrentDownload(ctx context.Context, rawurl, outPath string, concurrent int, verbose bool) error {
// 	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
// 	if err != nil {
// 		return err
// 	}

// 	resp, err := d.Client.Do(req)
// 	if err != nil {
// 		return err
// 	}
// 	defer resp.Body.Close()

// 	if resp.Header.Get("Accept-Ranges") != "bytes" {
// 		fmt.Println("Server does not support concurrent download, falling back to single thread")
// 		return d.singleDownload(ctx, rawurl, outPath, verbose)
// 	}

// 	totalSize, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
// 	if err != nil {
// 		return err
// 	}

// 	chunkSize := totalSize / int64(concurrent)
// 	var wg sync.WaitGroup
// 	var mu sync.Mutex
// 	var written int64

// 	startTime := time.Now()

// 	for i := 0; i < concurrent; i++ {
// 		wg.Add(1)
// 		start := int64(i) * chunkSize
// 		end := start + chunkSize - 1
// 		if i == concurrent-1 {
// 			end = totalSize - 1
// 		}

// 		go func(i int, start, end int64) {
// 			defer wg.Done()
// 			err := d.downloadChunk(ctx, rawurl, outPath, i, start, end, &mu, &written, totalSize, startTime, verbose)
// 			if err != nil {
// 				fmt.Fprintf(os.Stderr, "\nError downloading chunk %d: %v\n", i, err)
// 			}
// 		}(i, start, end)
// 	}

// 	wg.Wait()

// 	fmt.Print("Downloaded all parts! Merging...\n")

// 	// Merge files
// 	destFile, err := os.Create(outPath)
// 	if err != nil {
// 		return err
// 	}
// 	defer destFile.Close()

// 	for i := 0; i < concurrent; i++ {
// 		partFileName := fmt.Sprintf("%s.part%d", outPath, i)
// 		partFile, err := os.Open(partFileName)
// 		if err != nil {
// 			return err
// 		}
// 		_, err = io.Copy(destFile, partFile)
// 		if err != nil {
// 			partFile.Close()
// 			return err
// 		}
// 		partFile.Close()
// 		os.Remove(partFileName)
// 	}

// 	elapsed := time.Since(startTime)
// 	speed := float64(totalSize) / 1024.0 / elapsed.Seconds() // KiB/s
// 	fmt.Fprintf(os.Stderr, "\nDownloaded %s in %s (%s/s)\n", outPath, elapsed.Round(time.Second), util.ConvertBytesToHumanReadable(int64(speed*1024)))
// 	return nil
// }

// func (d *Downloader) downloadChunk(ctx context.Context, rawurl, outPath string, index int, start, end int64, mu *sync.Mutex, written *int64, totalSize int64, startTime time.Time, verbose bool) error {

// 	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
// 	if err != nil {
// 		return err
// 	}

// 	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
// 	resp, err := d.Client.Do(req)
// 	if err != nil {
// 		return err
// 	}
// 	defer resp.Body.Close()

// 	partFileName := fmt.Sprintf("%s.part%d", outPath, index)
// 	partFile, err := os.Create(partFileName)
// 	if err != nil {
// 		return err
// 	}
// 	defer partFile.Close()

// 	buf := make([]byte, 32*1024)
// 	for {

// 		n, err := resp.Body.Read(buf)

// 		if n > 0 {
// 			_, wErr := partFile.Write(buf[:n])
// 			if wErr != nil {
// 				return wErr
// 			}
// 			mu.Lock()
// 			*written += int64(n)
// 			d.printProgress(*written, totalSize, startTime, verbose)
// 			mu.Unlock()
// 		}
// 		if err == io.EOF {
// 			break
// 		}
// 		if err != nil {
// 			return err
// 		}
// 	}
// 	return nil
// }

// printProgress is defined in multi-downloader.go
