package web

import (
	"container/list"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mirkobrombin/go-foundation/v2/core/resiliency"
)

const (
	rateLimitClientTTL  = 10 * time.Minute
	rateLimitMaxClients = 10_000
)

type clientRateLimiter struct {
	key      string
	limiter  *resiliency.RateLimiter
	lastSeen time.Time
}

type clientRateLimitStore struct {
	mu          sync.Mutex
	clients     map[string]*list.Element
	recency     *list.List
	rate        int
	burst       int
	lastCleanup time.Time
}

// RateLimit returns nonblocking per-client rate limit middleware.
func RateLimit(rate, burst int) (Middleware, error) {
	if _, err := resiliency.NewRateLimiter(rate, burst); err != nil {
		return nil, err
	}
	store := &clientRateLimitStore{
		clients: make(map[string]*list.Element),
		recency: list.New(),
		rate:    rate,
		burst:   burst,
	}
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *Context) error {
			limiter, err := store.forClient(clientIdentity(ctx.Request), time.Now())
			if err != nil {
				return fmt.Errorf("web: rate limiter: %w", err)
			}
			if !limiter.Allow() {
				return Error(http.StatusTooManyRequests, "rate limit exceeded")
			}
			return next(ctx)
		}
	}, nil
}

func (s *clientRateLimitStore) forClient(key string, now time.Time) (*resiliency.RateLimiter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastCleanup.IsZero() || now.Sub(s.lastCleanup) >= rateLimitClientTTL {
		s.removeExpired(now)
		s.lastCleanup = now
	}
	if element, ok := s.clients[key]; ok {
		entry := element.Value.(*clientRateLimiter)
		entry.lastSeen = now
		s.recency.MoveToFront(element)
		return entry.limiter, nil
	}
	if len(s.clients) >= rateLimitMaxClients {
		s.removeElement(s.recency.Back())
	}
	limiter, err := resiliency.NewRateLimiter(s.rate, s.burst)
	if err != nil {
		return nil, err
	}
	entry := &clientRateLimiter{key: key, limiter: limiter, lastSeen: now}
	s.clients[key] = s.recency.PushFront(entry)
	return limiter, nil
}

func (s *clientRateLimitStore) removeExpired(now time.Time) {
	for element := s.recency.Back(); element != nil; {
		entry := element.Value.(*clientRateLimiter)
		if now.Sub(entry.lastSeen) <= rateLimitClientTTL {
			return
		}
		previous := element.Prev()
		s.removeElement(element)
		element = previous
	}
}

func (s *clientRateLimitStore) removeElement(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(*clientRateLimiter)
	delete(s.clients, entry.key)
	s.recency.Remove(element)
}

func clientIdentity(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	if request.RemoteAddr != "" {
		return request.RemoteAddr
	}
	return "unknown"
}
