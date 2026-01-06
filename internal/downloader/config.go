package downloader

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Size constants
const (
	KB = 1024
	MB = 1024 * KB
	GB = 1024 * MB

	// Megabyte as float for display calculations
	Megabyte = 1024.0 * 1024.0
)

// Chunk size constants for concurrent downloads
const (
	MinChunk     = 2 * MB  // Minimum chunk size
	MaxChunk     = 16 * MB // Maximum chunk size
	TargetChunk  = 8 * MB  // Target chunk size
	AlignSize    = 4 * KB  // Align chunks to 4KB for filesystem
	WorkerBuffer = 512 * KB

	TasksPerWorker = 4 // Target tasks per connection
)

// Connection limits
const (
	PerHostMax = 16 // Max concurrent connections per host
)

// HTTP Client Tuning
const (
	DefaultMaxIdleConns          = 100
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 15 * time.Second
	DefaultExpectContinueTimeout = 1 * time.Second
	DialTimeout                  = 10 * time.Second
	KeepAliveDuration            = 30 * time.Second
	ProbeTimeout                 = 30 * time.Second
)

// Channel buffer sizes
const (
	ProgressChannelBuffer = 100
)

// DownloadConfig contains all parameters needed to start a download
type DownloadConfig struct {
	URL        string
	OutputPath string
	ID         int
	Filename   string
	Verbose    bool
	MD5Sum     string
	SHA256Sum  string
	ProgressCh chan<- tea.Msg
	State      *ProgressState
}

const (
	maxTaskRetries = 3
	retryBaseDelay = 200 * time.Millisecond

	// Health check constants
	healthCheckInterval = 2 * time.Second // How often to check worker health
	slowWorkerThreshold = 0.30            // Restart if speed < x times of mean
	slowWorkerGrace     = 5 * time.Second // Grace period before checking speed
	speedEMAAlpha       = 0.2             // EMA smoothing factor
	minAbsoluteSpeed    = 100 * KB        // Don't cancel workers above this speed
)
