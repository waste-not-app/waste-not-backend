package photos

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Storage interface {
	Put(objectKey string, data []byte) error
	Get(objectKey string) ([]byte, error)
	Delete(objectKey string) error
}

type LocalStorage struct {
	baseDir string
}

func NewLocalStorage(baseDir string) (*LocalStorage, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, ErrPhotosDisabled
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &LocalStorage{baseDir: abs}, nil
}

func (s *LocalStorage) resolve(objectKey string) (string, error) {
	cleaned := filepath.Clean(objectKey)
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid object key")
	}
	target := filepath.Join(s.baseDir, cleaned)
	rel, err := filepath.Rel(s.baseDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("object key escapes base directory")
	}
	return target, nil
}

func (s *LocalStorage) Put(objectKey string, data []byte) error {
	target, err := s.resolve(objectKey)
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".photo-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func (s *LocalStorage) Get(objectKey string) ([]byte, error) {
	target, err := s.resolve(objectKey)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrPhotoNotFound
	}
	return data, err
}

func (s *LocalStorage) Delete(objectKey string) error {
	target, err := s.resolve(objectKey)
	if err != nil {
		return err
	}
	err = os.Remove(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
