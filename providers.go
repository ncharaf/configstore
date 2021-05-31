package configstore

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
)

// Logs functions can be overriden
var LogErrorFunc = log.Printf
var LogInfoFunc = log.Printf

/*
** DEFAULT PROVIDERS IMPLEMENTATION
 */

func errorProvider(s *Store, name string, err error) {
	if LogErrorFunc != nil {
		LogErrorFunc("error: %v", err)
	}
	s.RegisterProvider(name, newErrorProvider(err))
}

func newErrorProvider(err error) Provider {
	return func() (ItemList, error) {
		return ItemList{}, err
	}
}

func fileProvider(s *Store, filename string) {
	file(s, filename, false, nil)
}

func fileRefreshProvider(s *Store, filename string) {
	file(s, filename, true, nil)
}

func fileCustomProvider(s *Store, filename string, fn func([]byte) ([]Item, error)) {
	file(s, filename, false, fn)
}

func fileCustomRefreshProvider(s *Store, filename string, fn func([]byte) ([]Item, error)) {
	file(s, filename, true, fn)
}

func file(s *Store, filename string, refresh bool, fn func([]byte) ([]Item, error)) {

	if filename == "" {
		return
	}

	providername := fmt.Sprintf("file:%s", filename)

	last := time.Now()
	vals, err := readFile(filename, fn)
	if err != nil {
		errorProvider(s, providername, err)
		return
	}
	inmem := inMemoryProvider(s, providername)
	if LogInfoFunc != nil {
		LogInfoFunc("configuration from file: %s", filename)
	}
	inmem.Add(vals...)

	if refresh {
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			for range ticker.C {
				finfo, err := os.Stat(filename)
				if err != nil {
					continue
				}
				if finfo.ModTime().After(last) {
					last = finfo.ModTime()
				} else {
					continue
				}
				vals, err := readFile(filename, fn)
				if err != nil {
					continue
				}
				inmem.mut.Lock()
				inmem.items = vals
				inmem.mut.Unlock()
				s.NotifyWatchers()
			}
		}()
	}
}

func fileListProvider(s *Store, dirname string) {
	if dirname == "" {
		return
	}

	files, err := ioutil.ReadDir(dirname)
	if err != nil {
		errorProvider(s, fmt.Sprintf("filelist:%s", dirname), err)
		return
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			linkedFile, err := os.Stat(filepath.Join(dirname, file.Name()))
			if err != nil {
				errorProvider(s, fmt.Sprintf("filelist:%s", dirname), err)
				return
			}
			if linkedFile.IsDir() {
				continue
			}
		}

		fileProvider(s, filepath.Join(dirname, file.Name()))
	}
}

func readFile(filename string, fn func([]byte) ([]Item, error)) ([]Item, error) {
	vals := []Item{}
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	if fn != nil {
		return fn(b)
	}
	err = yaml.Unmarshal(b, &vals)
	if err != nil {
		return nil, err
	}
	return vals, nil
}

func inMemoryProvider(s *Store, name string) *InMemoryProvider {
	inmem := &InMemoryProvider{}
	s.RegisterProvider(name, inmem.Items)
	return inmem
}

// InMemoryProvider implements an in-memory configstore provider.
type InMemoryProvider struct {
	items []Item
	mut   sync.Mutex
}

// Add appends an item to the in-memory list.
func (inmem *InMemoryProvider) Add(s ...Item) *InMemoryProvider {
	inmem.mut.Lock()
	defer inmem.mut.Unlock()
	inmem.items = append(inmem.items, s...)
	return inmem
}

// Items returns the in-memory item list. This is the function that gets called by configstore.
func (inmem *InMemoryProvider) Items() (ItemList, error) {
	inmem.mut.Lock()
	defer inmem.mut.Unlock()
	return ItemList{Items: inmem.items}, nil
}

func envProvider(s *Store, prefix string) {

	if prefix != "" && !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	prefixName := strings.ToUpper(prefix)
	if prefixName == "" {
		prefixName = "all"
	}
	inmem := inMemoryProvider(s, fmt.Sprintf("env:%s", prefixName))

	prefix = transformKey(prefix)

	for _, e := range os.Environ() {
		ePair := strings.SplitN(e, "=", 2)
		if len(ePair) <= 1 {
			continue
		}
		eTr := transformKey(ePair[0])
		if strings.HasPrefix(eTr, prefix) {
			inmem.Add(NewItem(strings.TrimPrefix(eTr, prefix), ePair[1], 15))
		}
	}
}
