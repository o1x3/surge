package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// VerifyChecksum verifies the checksum of a given file against provided user checksums or server checksums.
// It returns an error if the checksums do not match or if there's an issue during calculation.
func VerifyChecksum(file *os.File, md5sum, sha256sum string, serverMD5, serverSHA256 string, verbose bool) error {
	// Ensure the file pointer is at the beginning before calculating checksum
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek to beginning of file for checksum verification: %w", err)
	}

	if md5sum != "" {
		if verbose {
			fmt.Fprintln(os.Stderr, "User provided MD5 checksum. Verifying...")
		}
		hasher := md5.New()
		if _, err := io.Copy(hasher, file); err != nil {
			return fmt.Errorf("failed to calculate MD5 checksum: %w", err)
		}
		calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
		if calculatedChecksum != md5sum {
			return fmt.Errorf("MD5 checksum mismatch: expected %s, got %s", md5sum, calculatedChecksum)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, "MD5 checksum verified successfully.")
		}
		return nil
	}

	if sha256sum != "" {
		if verbose {
			fmt.Fprintln(os.Stderr, "User provided SHA256 checksum. Verifying...")
		}
		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			return fmt.Errorf("failed to calculate SHA256 checksum: %w", err)
		}
		calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
		if calculatedChecksum != sha256sum {
			return fmt.Errorf("SHA256 checksum mismatch: expected %s, got %s", sha256sum, calculatedChecksum)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, "SHA256 checksum verified successfully.")
		}
		return nil
	}

	if serverMD5 != "" {
		if verbose {
			fmt.Fprintln(os.Stderr, "Server provided Content-MD5 checksum. Verifying...")
		}
		hasher := md5.New()
		if _, err := io.Copy(hasher, file); err != nil {
			return fmt.Errorf("failed to calculate checksum: %w", err)
		}
		calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
		if calculatedChecksum != serverMD5 {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", serverMD5, calculatedChecksum)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, "Checksum verified successfully.")
		}
		return nil
	}

	if serverSHA256 != "" {
		if verbose {
			fmt.Fprintln(os.Stderr, "Server provided X-Checksum-SHA256 checksum. Verifying...")
		}
		hasher := sha256.New()
		if _, err := io.Copy(hasher, file); err != nil {
			return fmt.Errorf("failed to calculate checksum: %w", err)
		}
		calculatedChecksum := hex.EncodeToString(hasher.Sum(nil))
		if calculatedChecksum != serverSHA256 {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", serverSHA256, calculatedChecksum)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, "Checksum verified successfully.")
		}
		return nil
	}

	if verbose {
		fmt.Fprintln(os.Stderr, "No checksum provided by server or user. Skipping verification.")
	}
	return nil
}
