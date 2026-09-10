package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const postgresMaxRetainedConnections = 8

type postgresConnectionRegistry struct {
	mutex   sync.Mutex
	entries map[string]*retainedPostgresConnection
	opener  postgresConnectionOpener
	now     func() time.Time
	closed  bool
}

type retainedPostgresConnection struct {
	mutex      sync.Mutex
	id         string
	resolved   postgresConnection
	handle     postgresConnectionHandle
	createdAt  time.Time
	lastUsedAt time.Time
	closed     bool
}

type postgresConnectionView struct {
	ID             string    `json:"id"`
	State          string    `json:"state"`
	Host           string    `json:"host"`
	Port           uint16    `json:"port"`
	Database       string    `json:"database,omitempty"`
	DatabaseSource string    `json:"database_source"`
	User           string    `json:"user"`
	CredentialType string    `json:"credential_type"`
	TLSMode        string    `json:"tls_mode"`
	Lifecycle      string    `json:"lifecycle"`
	CreatedAt      time.Time `json:"created_at"`
	LastUsedAt     time.Time `json:"last_used_at"`
}

func newPostgresConnectionRegistry(opener postgresConnectionOpener) *postgresConnectionRegistry {
	return &postgresConnectionRegistry{entries: make(map[string]*retainedPostgresConnection), opener: opener, now: time.Now}
}

func (registry *postgresConnectionRegistry) Create(ctx context.Context, resolved postgresConnection, target validatedTarget) (postgresConnectionView, diagnosticResult, error) {
	registry.mutex.Lock()
	if registry.closed {
		registry.mutex.Unlock()
		return postgresConnectionView{}, diagnosticResult{}, errors.New("connection_registry_closed")
	}
	if len(registry.entries) >= postgresMaxRetainedConnections {
		registry.mutex.Unlock()
		return postgresConnectionView{}, diagnosticResult{}, errors.New("connection_capacity_exceeded")
	}
	registry.mutex.Unlock()

	handle, failure := registry.opener.Open(ctx, resolved, target)
	if handle == nil {
		return postgresConnectionView{}, failure, nil
	}
	result := handle.Execute(ctx, "connect", resolved)
	if result.Status != "success" {
		_ = handle.Close(context.Background())
		return postgresConnectionView{}, result, nil
	}
	id, err := newPostgresConnectionID()
	if err != nil {
		_ = handle.Close(context.Background())
		return postgresConnectionView{}, diagnosticResult{}, errors.New("connection_id_unavailable")
	}
	now := registry.now().UTC()
	entry := &retainedPostgresConnection{id: id, resolved: resolved, handle: handle, createdAt: now, lastUsedAt: now}
	registry.mutex.Lock()
	if registry.closed {
		registry.mutex.Unlock()
		_ = handle.Close(context.Background())
		return postgresConnectionView{}, diagnosticResult{}, errors.New("connection_registry_closed")
	}
	if len(registry.entries) >= postgresMaxRetainedConnections {
		registry.mutex.Unlock()
		_ = handle.Close(context.Background())
		return postgresConnectionView{}, diagnosticResult{}, errors.New("connection_capacity_exceeded")
	}
	registry.entries[id] = entry
	registry.mutex.Unlock()
	return entry.view(), result, nil
}

func (registry *postgresConnectionRegistry) Get(id string) (postgresConnectionView, bool) {
	entry, ok := registry.lookup(id)
	if !ok {
		return postgresConnectionView{}, false
	}
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	return entry.viewLocked(), !entry.closed
}

func (registry *postgresConnectionRegistry) Execute(ctx context.Context, id, operation string) (diagnosticResult, bool) {
	entry, ok := registry.lookup(id)
	if !ok {
		return diagnosticResult{}, false
	}
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	if entry.closed {
		return diagnosticResult{}, false
	}
	result := entry.handle.Execute(ctx, operation, entry.resolved)
	entry.lastUsedAt = registry.now().UTC()
	return result, true
}

func (registry *postgresConnectionRegistry) Delete(ctx context.Context, id string) bool {
	registry.mutex.Lock()
	entry, ok := registry.entries[id]
	if ok {
		delete(registry.entries, id)
	}
	registry.mutex.Unlock()
	if !ok {
		return false
	}
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	entry.closed = true
	_ = entry.handle.Close(ctx)
	return true
}

func (registry *postgresConnectionRegistry) CloseAll(ctx context.Context) {
	registry.mutex.Lock()
	registry.closed = true
	entries := registry.entries
	registry.entries = make(map[string]*retainedPostgresConnection)
	registry.mutex.Unlock()
	for _, entry := range entries {
		entry.mutex.Lock()
		entry.closed = true
		_ = entry.handle.Close(ctx)
		entry.mutex.Unlock()
	}
}

func (registry *postgresConnectionRegistry) lookup(id string) (*retainedPostgresConnection, bool) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	entry, ok := registry.entries[id]
	return entry, ok
}

func (entry *retainedPostgresConnection) view() postgresConnectionView {
	entry.mutex.Lock()
	defer entry.mutex.Unlock()
	return entry.viewLocked()
}

func (entry *retainedPostgresConnection) viewLocked() postgresConnectionView {
	state := "ready"
	if entry.closed {
		state = "closed"
	}
	databaseSource := "server_selected"
	if entry.resolved.Database.Name != "" {
		databaseSource = "explicit"
	}
	return postgresConnectionView{
		ID: entry.id, State: state, Host: entry.resolved.Target.Host, Port: entry.resolved.Target.Port,
		Database: entry.resolved.Database.Name, DatabaseSource: databaseSource,
		User: entry.resolved.Identity.User, CredentialType: entry.resolved.Credential.Type,
		TLSMode: entry.resolved.TLS.Mode, Lifecycle: entry.resolved.Lifecycle.Mode,
		CreatedAt: entry.createdAt, LastUsedAt: entry.lastUsedAt,
	}
}

func newPostgresConnectionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
