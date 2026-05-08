package model

import (
	"os"
	"strings"
	"time"
)

const pinPrefix = "p_"

type Item struct {
	Name     string
	Preview  []string
	FullPath string
	Pinned   bool
	Size     int64
	Modified time.Time
}

func NewItem(fullPath string) (Item, error) {
	info, err := os.Stat(fullPath)
	if err != nil {
		return Item{}, err
	}

	name := info.Name()
	name = strings.TrimSuffix(name, ".md")
	pinned := strings.HasPrefix(name, pinPrefix)
	if pinned {
		name = strings.TrimPrefix(name, pinPrefix)
	}

	preview := []string{}
	content, err := os.ReadFile(fullPath)
	if err == nil {
		lines := strings.Split(string(content), "\n")
		for i := 0; i < len(lines) && i < 3; i++ {
			if lines[i] != "" {
				preview = append(preview, lines[i])
			}
		}
	}

	return Item{
		Name:     name,
		Preview:  preview,
		FullPath: fullPath,
		Pinned:   pinned,
		Size:     info.Size(),
		Modified: info.ModTime(),
	}, nil
}

func (i Item) DisplayName() string {
	return i.Name
}

func (i *Item) TogglePin() {
	i.Pinned = !i.Pinned
	if i.Pinned {
		i.Name = pinPrefix + i.Name
	} else {
		i.Name = strings.TrimPrefix(i.Name, pinPrefix)
	}
}

func (i *Item) Refresh() error {
	info, err := os.Stat(i.FullPath)
	if err != nil {
		return err
	}

	i.Size = info.Size()
	i.Modified = info.ModTime()

	content, err := os.ReadFile(i.FullPath)
	if err == nil {
		lines := strings.Split(string(content), "\n")
		i.Preview = []string{}
		for j := 0; j < len(lines) && j < 3; j++ {
			if lines[j] != "" {
				i.Preview = append(i.Preview, lines[j])
			}
		}
	}
	return nil
}

func SortItems(items []Item) []Item {
	result := make([]Item, len(items))
	copy(result, items)
	pinned := []Item{}
	unpinned := []Item{}
	for _, item := range result {
		if item.Pinned {
			pinned = append(pinned, item)
		} else {
			unpinned = append(unpinned, item)
		}
	}
	unpinned = sortByName(unpinned)
	return append(pinned, unpinned...)
}

func sortByName(items []Item) []Item {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Name < items[j-1].Name; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
	return items
}
