package services

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type StorageService struct {
	BasePath string
}

func NewStorageService(basePath string) *StorageService {
	return &StorageService{BasePath: basePath}
}

func (s *StorageService) SaveFile(assetType, channelID, filename string, reader io.Reader) (string, int64, string, error) {
	dir := filepath.Join(s.BasePath, "assets", assetType, channelID)
	os.MkdirAll(dir, 0755)

	destPath := filepath.Join(dir, filename)
	dest, err := os.Create(destPath)
	if err != nil {
		return "", 0, "", fmt.Errorf("create file: %w", err)
	}
	defer dest.Close()

	hash := sha256.New()
	multi := io.MultiWriter(dest, hash)
	written, err := io.Copy(multi, reader)
	if err != nil {
		os.Remove(destPath)
		return "", 0, "", fmt.Errorf("copy file: %w", err)
	}

	sha256Hex := fmt.Sprintf("%x", hash.Sum(nil))
	return destPath, written, sha256Hex, nil
}

func (s *StorageService) DeleteFile(filePath string) error {
	return os.Remove(filePath)
}

func (s *StorageService) GetFilePath(assetType, channelID, filename string) string {
	return filepath.Join(s.BasePath, "assets", assetType, channelID, filename)
}

func (s *StorageService) AssetDir(assetType, channelID string) string {
	dir := filepath.Join(s.BasePath, "assets", assetType, channelID)
	os.MkdirAll(dir, 0755)
	return dir
}

func (s *StorageService) DirSize(assetType, channelID string) (int64, int, error) {
	dir := filepath.Join(s.BasePath, "assets", assetType, channelID)
	var totalSize int64
	var fileCount int
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		totalSize += info.Size()
		fileCount++
		return nil
	})
	return totalSize, fileCount, nil
}