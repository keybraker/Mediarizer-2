package hash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// isImageFile checks if the file is an image based on its extension.
func isImageFile(filePath string) bool {
	lowerFilePath := strings.ToLower(filePath)
	return strings.HasSuffix(lowerFilePath, ".jpg") || strings.HasSuffix(lowerFilePath, ".jpeg") ||
		strings.HasSuffix(lowerFilePath, ".png") || strings.HasSuffix(lowerFilePath, ".gif") ||
		strings.HasSuffix(lowerFilePath, ".bmp") || strings.HasSuffix(lowerFilePath, ".tiff")
}

// calculateFileHash calculates the SHA-256 hash of the file at the given filePath.
func calculateFileHash(filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file at %s: %v", filePath, err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, fmt.Errorf("failed to calculate hash for file: %v", err)
	}

	return hash.Sum(nil), nil
}

// GetFileHash retrieves or calculates the hash of the file at filePath.
func GetFileHash(filePath string, hashCache *sync.Map) ([]byte, error) {
	if hash, found := hashCache.Load(filePath); found {
		return hash.([]byte), nil
	}

	calculatedHash, err := calculateFileHash(filePath)
	if err != nil {
		return nil, err
	}

	hashCache.Store(filePath, calculatedHash)
	return calculatedHash, nil
}

// hashImagesInPath hashes all images in the given path and updates the fileHashMap.
func HashImagesInPath(path string, hashCache *sync.Map, hashedFiles *int64) (*sync.Map, error) {
	fileHashMap := &sync.Map{}
	fileChan := make(chan string) // Channel to pass file paths to workers
	errChan := make(chan error)   // Channel to collect errors
	var wg sync.WaitGroup         // WaitGroup to track the worker goroutines

	numWorkers := runtime.NumCPU() / 2

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range fileChan {
				if isImageFile(filePath) {
					hashValue, err := GetFileHash(filePath, hashCache)
					if err != nil {
						errChan <- fmt.Errorf("failed to get file hash for %s: %v", filePath, err)
						return
					}

					hashStr := hex.EncodeToString(hashValue)
					fileHashMap.Store(hashStr, true)

					// Increment the hashed files counter
					atomic.AddInt64(hashedFiles, 1)
				}
			}
		}()
	}

	// Walk the directory and send file paths to the channel
	go func() {
		defer close(fileChan) // Close the channel when done
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				errChan <- fmt.Errorf("failed to walk path %s: %v", filePath, err)
				return err
			}

			if !info.IsDir() {
				fileChan <- filePath // Send file to channel for hashing
			}

			return nil
		})

		// If an error occurred during filepath walk, send it to the error channel
		if err != nil {
			errChan <- err
		}
	}()

	// Wait for all workers to finish
	go func() {
		wg.Wait()
		close(errChan) // Close error channel when all workers are done
	}()

	// Check for errors during execution
	for err := range errChan {
		if err != nil {
			return nil, err
		}
	}

	return fileHashMap, nil
}
