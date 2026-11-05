package cache

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

const MaxBytes int64 = 2 << 30
const MinFreeBytes uint64 = 1 << 30

var runName = regexp.MustCompile(`^run-[a-f0-9]{32}$`)

const marker = "yaneCompanion cache v1\n"

type Cache struct {
	root *os.Root
	Path string
}

func Open(path string) (*Cache, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("UNSAFE_CACHE")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &Cache{root, path}, nil
}

// Cleanup recognizes only our marker and name; Root confines all traversal and
// refuses symlink escapes. Unknown user files are never considered cache.
func (c *Cache) Cleanup() error {
	entries, err := fs.ReadDir(c.root.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !runName.MatchString(entry.Name()) || !entry.IsDir() {
			continue
		}
		path := filepath.Join(entry.Name(), ".owner")
		data, err := c.root.ReadFile(path)
		if err != nil || string(data) != marker {
			continue
		}
		if err := c.root.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cache) Create(name string) (string, error) {
	if !runName.MatchString(name) {
		return "", errors.New("INVALID_CACHE_NAME")
	}
	if err := c.root.Mkdir(name, 0700); err != nil {
		return "", err
	}
	if err := c.root.WriteFile(filepath.Join(name, ".owner"), []byte(marker), 0600); err != nil {
		return "", err
	}
	return filepath.Join(c.Path, name), nil
}

func (c *Cache) Check() error {
	var total int64
	err := fs.WalkDir(c.root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("UNSAFE_CACHE")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("UNSAFE_CACHE")
		}
		total += info.Size()
		if total > MaxBytes {
			return errors.New("CACHE_FULL")
		}
		return nil
	})
	if err != nil {
		return err
	}
	free, err := freeBytes(c.Path)
	if err != nil {
		return err
	}
	if free < MinFreeBytes {
		return errors.New("CACHE_FULL")
	}
	return nil
}
func (c *Cache) Close() error { return c.root.Close() }
