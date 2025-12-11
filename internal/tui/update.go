package tui

import (
	"context"
	"time"

	"surge/internal/downloader"
	"surge/internal/messages"

	tea "github.com/charmbracelet/bubbletea"
)

// Update handles messages and updates the model
func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case messages.DownloadStartedMsg:
		// Check if download already exists (by ID)
		var target *DownloadModel
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				target = d
				break
			}
		}
		if target != nil {
			// Update existing download with real metadata
			target.Filename = msg.Filename
			target.Total = msg.Total
			target.URL = msg.URL
		} else {
			// Should not happen if we optimistically added it, but fallback just in case
			newDownload := NewDownloadModel(msg.DownloadID, msg.URL, msg.Filename, msg.Total)
			m.downloads = append(m.downloads, newDownload)
		}
		cmds = append(cmds, listenForActivity(m.progressChan))

	case messages.ProgressMsg:
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				d.Downloaded = msg.Downloaded
				d.Speed = msg.Speed
				d.Elapsed = time.Since(d.StartTime)
				d.Connections = msg.ActiveConnections

				if d.Total > 0 {
					percentage := float64(d.Downloaded) / float64(d.Total)
					cmd := d.progress.SetPercent(percentage)
					cmds = append(cmds, cmd)
				}
				break
			}
		}

	case messages.DownloadCompleteMsg:
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				d.Downloaded = d.Total // Ensure we show 100%
				d.Elapsed = msg.Elapsed
				d.done = true
				break
			}
		}

	case messages.DownloadErrorMsg:
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				d.err = msg.Err
				d.done = true
				break
			}
		}

	case messages.TickMsg:
		cmds = append(cmds, tickCmd())

		// Also tick the progress bars if needed (they might have animations)
		// for _, d := range m.downloads {
		// 	 progress bars usually update on SetPercent, but some have internal unix ticks?
		// 	 standard bubbles progress doesn't need explicit ticks unless animating independently
		// }

	case tea.KeyMsg:
		switch m.state {
		case DashboardState:
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			if msg.String() == "g" {
				m.state = InputState
				m.focusedInput = 0
				m.inputs[0].SetValue("")
				m.inputs[1].SetValue(".")
				return m, nil
			}

			// Navigation
			if msg.String() == "up" || msg.String() == "k" {
				if m.cursor > 0 {
					m.cursor--
				}
			}
			if msg.String() == "down" || msg.String() == "j" {
				if m.cursor < len(m.downloads)-1 {
					m.cursor++
				}
			}

		case InputState:
			if msg.String() == "esc" {
				m.state = DashboardState
				return m, nil
			}
			if msg.String() == "enter" {
				if m.focusedInput == 0 {
					m.focusedInput = 1
					m.inputs[1].Focus()
					return m, nil
				}
				// Start download
				url := m.inputs[0].Value()
				path := m.inputs[1].Value()
				m.state = DashboardState

				// Optimistically add download
				nextID := len(m.downloads) + 1
				newDownload := NewDownloadModel(nextID, url, "Resolving...", 0)
				m.downloads = append(m.downloads, newDownload)

				return m, StartDownloadCmd(m.progressChan, nextID, url, path)
			}

			var cmd tea.Cmd
			m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
			return m, cmd
		}
	}

	return m, tea.Batch(cmds...)
}

func StartDownloadCmd(sub chan tea.Msg, id int, url, path string) tea.Cmd {
	return func() tea.Msg {
		d := downloader.NewDownloader()
		d.SetProgressChan(sub)
		d.SetID(id)

		ctx := context.Background()
		// We launch the download in a goroutine because tea.Cmd must return a Msg
		// But here, the downloader sends messages to the sub channel.
		// We don't need to return a Msg from this Cmd, the channel will handle it.
		// Wait, tea.Cmd expects a Msg return. We can return nil/AddDownloadMsg

		go func() {
			err := d.Download(ctx, url, path, 1, false, "", "") // Concurrency restricted to 1 as per user request
			if err != nil {
				sub <- messages.DownloadErrorMsg{DownloadID: id, Err: err}
			}
		}()

		return nil
	}
}
