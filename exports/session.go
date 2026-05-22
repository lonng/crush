package exports

import (
	"context"
	"sync"

	"github.com/charmbracelet/crush/internal/session"
)

type sessionService struct {
	session.Service
	mu        sync.RWMutex
	current   string
	sessionID string
}

func newSessionService(internal session.Service, sessionID string) *sessionService {
	return &sessionService{Service: internal, sessionID: sessionID}
}

func (ss *sessionService) Create(ctx context.Context, title string) (session.Session, error) {
	if ss.sessionID != "" {
		s, err := ss.Service.Get(ctx, ss.sessionID)
		if err == nil {
			ss.setCurrent(s.ID)
			return s, nil
		}
		s, err = ss.Service.CreateWithID(ctx, ss.sessionID, title)
		if err == nil {
			ss.setCurrent(s.ID)
		}
		return s, err
	}

	s, err := ss.Service.Create(ctx, title)
	if err == nil {
		ss.setCurrent(s.ID)
	}
	return s, err
}

func (ss *sessionService) Get(ctx context.Context, id string) (session.Session, error) {
	s, err := ss.Service.Get(ctx, id)
	if err == nil && s.ParentSessionID == "" {
		ss.setCurrent(s.ID)
	}
	return s, err
}

func (ss *sessionService) GetLast(ctx context.Context) (session.Session, error) {
	s, err := ss.Service.GetLast(ctx)
	if err == nil {
		ss.setCurrent(s.ID)
	}
	return s, err
}

func (ss *sessionService) CurrentSessionID() string {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.current
}

func (ss *sessionService) SetCurrentSessionID(id string) {
	ss.setCurrent(id)
}

func (ss *sessionService) setCurrent(id string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.current = id
}
