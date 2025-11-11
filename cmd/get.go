package cmd

import (
	"context"
	"fmt"
	"os"

	"surge/internal/downloader"

	"github.com/spf13/cobra"
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

		if outPath == "" {
			outPath = "." // Default download directory to current directory
		}

		d := downloader.NewDownloader()
		ctx := context.Background()

		fmt.Printf("Downloading %s to %s...\n", url, outPath)
		err := d.Download(ctx, url, outPath, concurrent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error downloading file: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Download complete!")
	},
}

func init() {
	getCmd.Flags().StringP("path", "p", "", "the path to the download folder")
	getCmd.Flags().IntP("concurrent", "c", 1, "number of concurrent downloads")
}
