package server

import "sync"

// SessionStore defines an interface for overriding the in-process session
// storage.
type SessionStore interface {
	Load(key string) (ClientSession, bool)
	LoadAndDelete(key string) (ClientSession, bool)
	LoadOrStore(string, ClientSession) (ClientSession, bool)
	Range(f func(string, ClientSession) bool)
}

type localSessionStore struct {
	sync.Map
}

func (store *localSessionStore) Load(key string) (ClientSession, bool) {
	value, ok := store.Map.Load(key)
	if !ok {
		return nil, false
	}

	return value.(ClientSession), true
}

func (store *localSessionStore) LoadAndDelete(key string) (ClientSession, bool) {
	value, loaded := store.Map.LoadAndDelete(key)
	if !loaded {
		return nil, false
	}

	return value.(ClientSession), true
}

func (store *localSessionStore) LoadOrStore(key string, value ClientSession) (ClientSession, bool) {
	actual, loaded := store.Map.LoadOrStore(key, value)
	if !loaded {
		return value, false
	}

	return actual.(ClientSession), true
}

func (store *localSessionStore) Range(f func(string, ClientSession) bool) {
	store.Map.Range(func(key any, value any) bool {
		return f(key.(string), value.(ClientSession))
	})
}
