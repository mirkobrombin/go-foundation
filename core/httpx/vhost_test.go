package httpx

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type recordingHooks struct {
	name   string
	handle bool
	events *[]string
}

func (h recordingHooks) BeforeRequest(r *http.Request) {
	*h.events = append(*h.events, h.name+":before")
}

func (h recordingHooks) HandleRequest(w http.ResponseWriter, r *http.Request) bool {
	*h.events = append(*h.events, h.name+":handle")
	return h.handle
}

func (h recordingHooks) AfterRequest(w http.ResponseWriter, r *http.Request) {
	*h.events = append(*h.events, h.name+":after")
}

func TestVHostMuxDispatchesByHostIgnoringCaseAndPort(t *testing.T) {
	mux := NewVHostMux()
	called := false
	mux.AddVirtualHost(VirtualHost{
		Host: "Example.COM",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusTeapot)
		}),
	})

	for _, host := range []string{"example.com", "EXAMPLE.com:8443"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusTeapot {
			t.Errorf("host %q: got %d, want %d", host, rec.Code, http.StatusTeapot)
		}
	}
	if !called {
		t.Error("virtual host handler was not called")
	}
}

func TestVHostMuxUnknownHost(t *testing.T) {
	mux := NewVHostMux()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "unknown.example"
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.Code)
	}

	mux = NewVHostMux(WithFallback(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("fallback: got %d, want 502", rec.Code)
	}
}

func TestVHostMuxHookOrderAndShortCircuit(t *testing.T) {
	var events []string
	mux := NewVHostMux()
	mux.AddVirtualHost(VirtualHost{
		Host: "a.example",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			events = append(events, "vhost:handler")
		}),
		Hooks: []Hooks{
			recordingHooks{name: "first", handle: true, events: &events},
			recordingHooks{name: "second", events: &events},
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "a.example"
	mux.ServeHTTP(rec, req)

	want := []string{
		"first:before", "second:before",
		"first:handle",                // short-circuits: no second:handle, no site:handler
		"second:after", "first:after", // reverse order
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("got %v, want %v", events, want)
	}
}

func TestVHostMuxHooksPassThroughToHandler(t *testing.T) {
	var events []string
	mux := NewVHostMux()
	mux.AddVirtualHost(VirtualHost{
		Host: "a.example",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			events = append(events, "vhost:handler")
		}),
		Hooks: []Hooks{recordingHooks{name: "p", events: &events}},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "a.example"
	mux.ServeHTTP(rec, req)

	want := []string{"p:before", "p:handle", "vhost:handler", "p:after"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("got %v, want %v", events, want)
	}
}

func TestVHostMuxRemoveSite(t *testing.T) {
	mux := NewVHostMux()
	mux.AddVirtualHost(VirtualHost{Host: "a.example", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})})
	if got := mux.Hosts(); !reflect.DeepEqual(got, []string{"a.example"}) {
		t.Fatalf("hosts: %v", got)
	}
	if !mux.RemoveVirtualHost("A.example:443") {
		t.Fatal("RemoveVirtualHost should report removal with normalized host")
	}
	if mux.RemoveVirtualHost("a.example") {
		t.Fatal("RemoveVirtualHost should report false for unknown host")
	}
}

func TestCertResolverSNI(t *testing.T) {
	certFile, keyFile := writeSelfSigned(t, "a.example")

	r := NewCertResolver()
	if err := r.Add("A.example", certFile, keyFile); err != nil {
		t.Fatal(err)
	}

	cert, err := r.GetCertificate(&tls.ClientHelloInfo{ServerName: "a.example"})
	if err != nil || cert == nil {
		t.Fatalf("SNI lookup failed: %v", err)
	}
	if _, err := r.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.example"}); err == nil {
		t.Fatal("expected error for unknown SNI name")
	}
	if !r.Remove("a.example") {
		t.Fatal("expected Remove to succeed")
	}
	if _, err := r.GetCertificate(&tls.ClientHelloInfo{ServerName: "a.example"}); err == nil {
		t.Fatal("expected error after removal")
	}
}

func TestCertResolverRejectsBadPair(t *testing.T) {
	r := NewCertResolver()
	if err := r.Add("a.example", "/nonexistent/cert.pem", "/nonexistent/key.pem"); err == nil {
		t.Fatal("expected error for missing files")
	} else if got := err.Error(); !containsSub(got, "a.example") {
		t.Errorf("error %q does not name the host", got)
	}
}

func containsSub(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && fmt.Sprintf("%s", s) != "" && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
