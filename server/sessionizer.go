package server

import "sync"

type Sessionizer interface {
	LoadOrStore(sessionID string, session ClientSession) (ClientSession, bool)

	Delete(sessionID string)

	All() []ClientSession
}

type SyncMapSessionizer struct {
	sessions sync.Map
}

var _ Sessionizer = (*SyncMapSessionizer)(nil)

func (s *SyncMapSessionizer) LoadOrStore(sessionID string, session ClientSession) (ClientSession, bool) {
	actual, ok := s.sessions.LoadOrStore(sessionID, session)
	if ok {
		return actual.(ClientSession), true
	}
	return session, false
}

func (s *SyncMapSessionizer) Delete(sessionID string) {
	s.sessions.Delete(sessionID)
}

func (s *SyncMapSessionizer) All() []ClientSession {
	var sessions []ClientSession
	s.sessions.Range(func(key, value any) bool {
		sessions = append(sessions, value.(ClientSession))
		return true
	})
	return sessions
}
