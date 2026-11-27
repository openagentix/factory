package server

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func baseCfg() Config {
	return Config{Port: testPort, Mode: "demo", Static: fstest.MapFS{"index.html": {Data: []byte("x")}}}
}

func TestNewRefusesAnythingButLoopbackAndAGoodConfig(t *testing.T) {
	for _, h := range []string{"0.0.0.0", "::", "::1", "[::1]", "192.168.1.5", "10.0.0.1", "localhost", "127.0.0.2", "127.0.0.1:80", " 127.0.0.1", "127.0.0.1 ", "example.com", "0", "[::]"} {
		c := baseCfg()
		c.Host = h
		if _, err := New(c); err == nil {
			t.Errorf("Host %q accepted", h)
		}
	}
	for _, h := range []string{"", "127.0.0.1"} {
		c := baseCfg()
		c.Host = h
		if _, err := New(c); err != nil {
			t.Errorf("Host %q: %v", h, err)
		}
	}
	for _, p := range []int{0, -1, 80, 1023, 65536, 70000} {
		c := baseCfg()
		c.Port = p
		if _, err := New(c); err == nil {
			t.Errorf("Port %d accepted", p)
		}
	}
	for _, p := range []int{1024, 65535} {
		c := baseCfg()
		c.Port = p
		if _, err := New(c); err != nil {
			t.Errorf("Port %d: %v", p, err)
		}
	}
	for _, m := range []string{"", "x", "DEMO", "Live", "demo "} {
		c := baseCfg()
		c.Mode = m
		if _, err := New(c); err == nil {
			t.Errorf("Mode %q accepted", m)
		}
	}
	c := baseCfg()
	c.Static = nil
	if _, err := New(c); err == nil {
		t.Error("nil Static accepted")
	}
	for _, f := range []func(*Config){
		func(c *Config) { c.PingInterval = -time.Second },
		func(c *Config) { c.StreamMaxAge = -time.Second },
		func(c *Config) { c.WriteDeadline = -time.Second },
	} {
		c := baseCfg()
		f(&c)
		if _, err := New(c); err == nil {
			t.Error("a negative duration was accepted")
		}
	}
}

func TestListenBindsOnlyLoopbackAndABusyPortIsAnError(t *testing.T) {
	port := freePort(t)
	c := baseCfg()
	c.Port = port
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := s.Listen()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ln.Addr().String(), fmt.Sprintf("127.0.0.1:%d", port); got != want {
		t.Errorf("bound %s, want %s", got, want)
	}
	// the port is now busy: a second server must fail and not move
	s2, _ := New(c)
	ln2, err := s2.Listen()
	if err == nil {
		ln2.Close()
		t.Fatal("a busy port was bound twice")
	}
	if strings.Contains(err.Error(), s2.Link()) || strings.Contains(err.Error(), string(s2.key)) {
		t.Error("error holds the key")
	}
	// the other ports near it stay free: nothing else was tried
	for _, p := range []int{port - 1, port + 1} {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), 5*time.Second); err == nil {
			c.Close()
			t.Errorf("something listens on %d", p)
		}
	}
	ln.Close()
}

func TestServeRefusesAListenerThatIsNotTheConfiguredAddress(t *testing.T) {
	c := baseCfg()
	c.Port = freePort(t) // not the port of the listener below
	s, _ := New(c)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	err = s.Serve(t.Context(), ln)
	if err == nil {
		t.Fatal("served on the wrong port")
	}
	if strings.Contains(err.Error(), string(s.key)) {
		t.Error("error holds the key")
	}
}
