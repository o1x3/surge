package cmd

import (
	"context"
	"fmt"
	"os"

	"surge/internal/downloader"

	"surge/internal/messages"
	"surge/internal/tui"

	"github.com/spf13/cobra"

	tea "github.com/charmbracelet/bubbletea"
)

var getCmd = &cobra.Command{
	Use:   "get [url]",
	Short: "get downloads a file from a URL",
	Long:  `get downloads a file from a URL and saves it to the local filesystem.`,
	Args:  cobra.ExactArgs(1), // Ensures that exactly one argument (the URL) is provided
	Run: func(cmd *cobra.Command, args []string) {
		url := args[0]
		outPath, _ := cmd.Flags().GetString("path")
		concurrent, _ := cmd.Flags().GetInt("concurrent")
		verbose, _ := cmd.Flags().GetBool("verbose")
		md5sum, _ := cmd.Flags().GetString("md5")
		sha256sum, _ := cmd.Flags().GetString("sha256")

		if outPath == "" {
			outPath = "." // Default download directory to current directory
		}

		d := downloader.NewDownloader()
		ctx := context.Background()

		// Initialize Bubble Tea program
		p := tea.NewProgram(tui.InitialRootModel(), tea.WithAltScreen())

		// Create a channel for progress updates
		progressCh := make(chan tea.Msg, 100)
		d.SetProgressChan(progressCh)
		d.SetID(1) // Single download for now

		// Start a goroutine to pump messages from channel to program
		go func() {
			for msg := range progressCh {
				p.Send(msg)
			}
		}()

		// Start download in a goroutine
		go func() {
			defer close(progressCh)
			// fmt.Printf("Downloading %s to %s...\n", url, outPath) // Removed printing to stdout to rely on TUI
			err := d.Download(ctx, url, outPath, concurrent, verbose, md5sum, sha256sum)
			if err != nil {
				p.Send(messages.DownloadErrorMsg{DownloadID: 1, Err: err})
			}
		}()

		// Run the TUI
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	getCmd.Flags().StringP("path", "p", "", "the path to the download folder")
	getCmd.Flags().IntP("concurrent", "c", 1, "number of concurrent connections (1 = single thread)")
	getCmd.Flags().BoolP("verbose", "v", false, "enable verbose output")
	getCmd.Flags().String("md5", "", "MD5 checksum for verification")
	getCmd.Flags().String("sha256", "", "SHA256 checksum for verification")
}
