package httpx

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/mirkobrombin/go-foundation/v2/core/options"
)

// Hooks is the request lifecycle extension contract.
//
// Example:
//
//	type Auth struct{ ... }
//	func (a Auth) BeforeRequest(r *http.Request)              { ... }
//	func (a Auth) HandleRequest(w, r) bool                    { ... }
//	func (a Auth) AfterRequest(w, r)                          { ... }
type Hooks interface {
	// BeforeRequest is invoked before serving each request.
	BeforeRequest(r *http.Request)
	// HandleRequest can fully handle the request, returning true if it does so.
	HandleRequest(w http.ResponseWriter, r *http.Request) (handled bool)
	// AfterRequest is invoked after the request has been served or handled.
	AfterRequest(w http.ResponseWriter, r *http.Request)
}

// VirtualHost binds one host name to a handler and its lifecycle hooks.
//
// Host matching is case-insensitive and ignores the request port: a virtual
// host registered as "example.com" answers "example.com", "EXAMPLE.com" and
// "example.com:8443".
type VirtualHost struct {
	// Host is the host name this virtual host answers, without port.
	Host string
	// Handler serves requests the hooks did not handle.
	Handler http.Handler
	// Hooks run around Handler in registration order for BeforeRequest and
	// HandleRequest, and in reverse order for AfterRequest (like a stack).
	Hooks []Hooks
}

// VHostMux dispatches requests to virtual hosts by Host header. It is the
// counterpart of a path router for the multi-tenant shape: instead of
// path-based routes for one application, it routes host names to independent
// handlers in one process.
type VHostMux struct {
	mu       sync.RWMutex
	hosts    map[string]*VirtualHost
	fallback http.Handler
}

// Option configures a VHostMux.
type Option = options.Option[VHostMux]

// WithFallback sets the handler used when no virtual host matches the request
// host. Without a fallback, unmatched hosts get 404.
func WithFallback(h http.Handler) Option {
	return func(m *VHostMux) { m.fallback = h }
}

// NewVHostMux creates an empty mux.
func NewVHostMux(opts ...Option) *VHostMux {
	m := &VHostMux{hosts: make(map[string]*VirtualHost)}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// AddVirtualHost registers a virtual host. Registering the same host twice
// replaces the previous one. Host is normalized (lowercased, port stripped).
func (m *VHostMux) AddVirtualHost(vh VirtualHost) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hosts[normalizeHost(vh.Host)] = &vh
}

// RemoveVirtualHost unregisters the virtual host for host, returning whether
// one existed.
func (m *VHostMux) RemoveVirtualHost(host string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := normalizeHost(host)
	if _, ok := m.hosts[key]; !ok {
		return false
	}
	delete(m.hosts, key)
	return true
}

// Hosts returns the sorted list of registered host names.
func (m *VHostMux) Hosts() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	hosts := make([]string, 0, len(m.hosts))
	for h := range m.hosts {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts
}

// ServeHTTP implements http.Handler.
func (m *VHostMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.RLock()
	vh, ok := m.hosts[normalizeHost(r.Host)]
	fallback := m.fallback
	m.mu.RUnlock()

	if !ok {
		if fallback != nil {
			fallback.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	for _, h := range vh.Hooks {
		h.BeforeRequest(r)
	}

	handled := false
	for _, h := range vh.Hooks {
		if h.HandleRequest(w, r) {
			handled = true
			break
		}
	}

	if !handled && vh.Handler != nil {
		vh.Handler.ServeHTTP(w, r)
	}

	for i := len(vh.Hooks) - 1; i >= 0; i-- {
		vh.Hooks[i].AfterRequest(w, r)
	}
}

// normalizeHost lowercases and strips any port from a host value.
func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// CertResolver maps host names to TLS key pairs, providing SNI-based
// certificate selection for a listener that terminates TLS for many host
// names.
type CertResolver struct {
	mu    sync.RWMutex
	certs map[string]*tls.Certificate
}

// NewCertResolver creates an empty resolver.
func NewCertResolver() *CertResolver {
	return &CertResolver{certs: make(map[string]*tls.Certificate)}
}

// Add loads a certificate and key from disk and registers them for host.
// Host is normalized the same way as VHostMux hosts.
func (r *CertResolver) Add(host, certFile, keyFile string) error {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("httpx: certificate for %s: %w", host, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.certs[normalizeHost(host)] = &cert
	return nil
}

// Remove drops the certificate for host, returning whether one existed.
func (r *CertResolver) Remove(host string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := normalizeHost(host)
	if _, ok := r.certs[key]; !ok {
		return false
	}
	delete(r.certs, key)
	return true
}

// GetCertificate implements the tls.Config.GetCertificate callback: it
// returns the certificate registered for the SNI server name, or nil (which
// fails the handshake) when no host name matches.
func (r *CertResolver) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cert, ok := r.certs[normalizeHost(hello.ServerName)]
	if !ok {
		return nil, fmt.Errorf("httpx: no certificate for %q", hello.ServerName)
	}
	return cert, nil
}
