package tui

import (
	"fmt"
	"time"

	"surge/internal/utils"

	"github.com/charmbracelet/lipgloss"
)

// View renders the entire TUI
func (m RootModel) View() string {
	if m.state == InputState {
		// Centered popup
		popup := lipgloss.JoinVertical(lipgloss.Left,
			TitleStyle.Render("Add New Download"),
			"",
			AppStyle.Render("URL:"),
			m.inputs[0].View(),
			"",
			AppStyle.Render("Path:"),
			m.inputs[1].View(),
			"",
			lipgloss.NewStyle().Foreground(ColorSubtext).Render("[Enter] Start  [Esc] Cancel"),
		)

		// Use Place to center the popup
		width := m.width
		if width == 0 {
			width = 80
		}
		height := m.height
		if height == 0 {
			height = 24
		}

		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
			PanelStyle.Padding(2, 4).Render(popup),
		)
	}

	if len(m.downloads) == 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.JoinVertical(lipgloss.Center,
				TitleStyle.Render("Surge"),
				"",
				"No active downloads.",
				"",
				"[g] Add Download  [q] Quit",
			),
		)
	}

	// === List Panel ===
	var listItems []string
	for i, d := range m.downloads {
		style := ItemStyle
		marker := "  "
		if i == m.cursor {
			style = SelectedItemStyle
			marker = "> "
		}

		// Progress bar for list item
		pct := 0.0
		if d.Total > 0 {
			pct = float64(d.Downloaded) / float64(d.Total)
		}
		status := "⬇"
		if d.done {
			status = "✓"
		}
		if d.err != nil {
			status = "✗"
		}

		line := fmt.Sprintf("%s%s %s (%.0f%%)", marker, status, d.Filename, pct*100)
		listItems = append(listItems, style.Render(line))
	}

	listContent := lipgloss.JoinVertical(lipgloss.Left, listItems...)
	listPanel := FocusedPanelStyle.Width(35).Height(m.height - 4).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Render("Downloads"),
			"",
			listContent,
		),
	)

	// === Details Panel ===
	selected := m.downloads[m.cursor]
	detailsContent := renderDetails(selected)
	detailsPanel := PanelStyle.Width(m.width - 40).Height(m.height - 4).Render(detailsContent)

	// === Status Bar ===
	statusBar := StatusBarStyle.Width(m.width).Render(
		fmt.Sprintf(" [g] Add Download  [q] Quit  |  Total Speed: %.1f MB/s", m.calcTotalSpeed()),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, listPanel, detailsPanel),
		statusBar,
	)
}

func renderDetails(m *DownloadModel) string {

	title := TitleStyle.Render(m.Filename)

	if m.err != nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			lipgloss.NewStyle().Foreground(ColorError).Render(fmt.Sprintf("Error: %v", m.err)),
		)
	}

	// Calculate stats
	percentage := 0.0
	if m.Total > 0 {
		percentage = float64(m.Downloaded) / float64(m.Total)
	}

	eta := "N/A"
	if m.Speed > 0 && m.Total > 0 {
		remainingBytes := m.Total - m.Downloaded
		remainingSeconds := float64(remainingBytes) / m.Speed
		eta = time.Duration(remainingSeconds * float64(time.Second)).Round(time.Second).String()
	}

	// Progress Bar
	m.progress.Width = 40
	progressBar := m.progress.View()

	stats := lipgloss.JoinVertical(lipgloss.Left,
		fmt.Sprintf("Progress:    %.1f%%", percentage*100),
		fmt.Sprintf("Size:        %s / %s", utils.ConvertBytesToHumanReadable(m.Downloaded), utils.ConvertBytesToHumanReadable(m.Total)),
		fmt.Sprintf("Speed:       %.1f MB/s", m.Speed/1024.0/1024.0),
		fmt.Sprintf("ETA:         %s", eta),
		fmt.Sprintf("Connections: %d", m.Connections),
		fmt.Sprintf("Elapsed:     %s", m.Elapsed.Round(time.Second)),
		fmt.Sprintf("URL:         %s", m.URL),
	)

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		progressBar,
		"",
		stats,
	)
}

func (m RootModel) calcTotalSpeed() float64 {
	total := 0.0
	for _, d := range m.downloads {
		total += d.Speed
	}
	return total / 1024.0 / 1024.0
}
