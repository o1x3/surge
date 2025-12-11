package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"surge/internal/messages"
)

type UIState int

const (
	DashboardState UIState = iota
	InputState
)

type DownloadModel struct {
	ID          int
	URL         string
	Filename    string
	Total       int64
	Downloaded  int64
	Speed       float64
	Connections int

	StartTime time.Time
	Elapsed   time.Duration

	progress progress.Model

	done bool
	err  error
}

type RootModel struct {
	downloads    []*DownloadModel
	width        int
	height       int
	state        UIState
	inputs       []textinput.Model
	focusedInput int
	progressChan chan tea.Msg // Single channel for all downloads

	// Navigation
	cursor int
}

// NewDownloadModel creates a new download model
func NewDownloadModel(id int, url string, filename string, total int64) *DownloadModel {
	return &DownloadModel{
		ID:        id,
		URL:       url,
		Filename:  filename,
		Total:     total,
		StartTime: time.Now(),
		progress:  progress.New(progress.WithDefaultGradient()),
	}
}

func InitialRootModel() RootModel {
	// Initialize inputs
	urlInput := textinput.New()
	urlInput.Placeholder = "https://example.com/file.zip"
	urlInput.Focus()
	urlInput.Width = 50
	urlInput.Prompt = "URL: "

	pathInput := textinput.New()
	pathInput.Placeholder = "."
	pathInput.Width = 50
	pathInput.Prompt = "Out: "

	return RootModel{
		downloads:    make([]*DownloadModel, 0),
		inputs:       []textinput.Model{urlInput, pathInput},
		state:        DashboardState,
		progressChan: make(chan tea.Msg, 100),
	}
}

func (m RootModel) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		listenForActivity(m.progressChan),
	)
}

func listenForActivity(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(_ time.Time) tea.Msg {
		return messages.TickMsg{}
	})
}
