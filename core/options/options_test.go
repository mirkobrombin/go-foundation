package options

import "testing"

type ServerConfig struct {
	Host    string
	Port    int
	Timeout int
}

func WithHost(h string) func(*ServerConfig) {
	return func(c *ServerConfig) { c.Host = h }
}

func WithPort(p int) func(*ServerConfig) {
	return func(c *ServerConfig) { c.Port = p }
}

func WithTimeout(t int) func(*ServerConfig) {
	return func(c *ServerConfig) { c.Timeout = t }
}

func TestApply(t *testing.T) {
	cfg := &ServerConfig{Host: "localhost", Port: 8080}

	Apply(cfg, WithHost("0.0.0.0"), WithPort(9000))

	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host: got %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.Port != 9000 {
		t.Errorf("Port: got %d, want %d", cfg.Port, 9000)
	}
}

func TestBuilder(t *testing.T) {
	defaults := ServerConfig{Host: "localhost", Port: 8080, Timeout: 30}

	cfg := NewBuilder(defaults).
		With(WithHost("127.0.0.1")).
		With(WithTimeout(60)).
		Build()

	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host: got %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.Port != 8080 {
		t.Errorf("Port should remain default: got %d, want %d", cfg.Port, 8080)
	}
	if cfg.Timeout != 60 {
		t.Errorf("Timeout: got %d, want %d", cfg.Timeout, 60)
	}
}

func TestBuilder_Ptr(t *testing.T) {
	defaults := ServerConfig{Port: 3000}

	cfg := NewBuilder(defaults).
		With(WithHost("api.example.com")).
		Ptr()

	if cfg.Host != "api.example.com" {
		t.Errorf("Host: got %q, want %q", cfg.Host, "api.example.com")
	}
	if cfg.Port != 3000 {
		t.Errorf("Port: got %d, want %d", cfg.Port, 3000)
	}
}
