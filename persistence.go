package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxPersistenceMarkerBytes = 4 * 1024

type persistenceStore struct {
	path  string
	mutex sync.Mutex
}

type persistenceInput struct {
	Value string `json:"value"`
}

type persistenceState struct {
	Exists bool   `json:"exists"`
	Size   int    `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

func newPersistenceStore(path string) (*persistenceStore, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) || path == string(filepath.Separator) {
		return nil, errors.New("TESTKIT_PERSISTENCE_FILE must be an absolute file path")
	}
	return &persistenceStore{path: path}, nil
}

func (store *persistenceStore) handler(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	switch request.Method {
	case http.MethodGet:
		state, err := store.read()
		if err != nil {
			http.Error(writer, "persistent marker unavailable", http.StatusInternalServerError)
			return
		}
		writeJSON(writer, http.StatusOK, state)
	case http.MethodPut:
		var input persistenceInput
		if err := decodeJSONBody(writer, request, maxJSONBodyBytes, &input); err != nil {
			return
		}
		if len(input.Value) == 0 || len(input.Value) > maxPersistenceMarkerBytes {
			http.Error(writer, "marker must contain 1 to 4096 bytes", http.StatusBadRequest)
			return
		}
		state, err := store.write([]byte(input.Value))
		if err != nil {
			http.Error(writer, "persistent marker unavailable", http.StatusInternalServerError)
			return
		}
		writeJSON(writer, http.StatusOK, state)
	default:
		writer.Header().Set("Allow", "GET, PUT")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (store *persistenceStore) read() (persistenceState, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	file, err := os.Open(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return persistenceState{}, nil
	}
	if err != nil {
		return persistenceState{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxPersistenceMarkerBytes+1))
	if err != nil {
		return persistenceState{}, err
	}
	if len(data) > maxPersistenceMarkerBytes {
		return persistenceState{}, fmt.Errorf("persistent marker exceeds %d bytes", maxPersistenceMarkerBytes)
	}
	return markerState(data), nil
}

func (store *persistenceStore) write(data []byte) (persistenceState, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	directory := filepath.Dir(store.path)
	info, err := os.Stat(directory)
	if err != nil {
		return persistenceState{}, err
	}
	if !info.IsDir() {
		return persistenceState{}, errors.New("persistent marker parent is not a directory")
	}
	temporary, err := os.CreateTemp(directory, ".testkit-marker-*")
	if err != nil {
		return persistenceState{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return persistenceState{}, err
	}
	if err = os.Rename(temporaryPath, store.path); err != nil {
		return persistenceState{}, err
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return persistenceState{}, err
	}
	err = directoryHandle.Sync()
	if closeErr = directoryHandle.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return persistenceState{}, err
	}
	return markerState(data), nil
}

func markerState(data []byte) persistenceState {
	digest := sha256.Sum256(data)
	return persistenceState{Exists: true, Size: len(data), SHA256: hex.EncodeToString(digest[:])}
}
