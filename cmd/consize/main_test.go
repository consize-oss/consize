package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestUnauthenticatedServerIsLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080"} {
		if err := validateListenAddress(addr, false); err != nil {
			t.Fatalf("loopback %s rejected: %v", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "192.0.2.10:8080"} {
		if err := validateListenAddress(addr, false); err == nil {
			t.Fatalf("unauthenticated non-loopback %s accepted", addr)
		}
		if err := validateListenAddress(addr, true); err != nil {
			t.Fatalf("authenticated address %s rejected: %v", addr, err)
		}
	}
}

func TestPostgresStorageCommandTimesOutWhenConnectionStalls(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		select {
		case conn := <-accepted:
			_ = conn.Close()
		default:
		}
	})

	start := time.Now()
	err = runStorage(context.Background(), []string{
		"status",
		"-database-url", fmt.Sprintf("postgres://consize:test-password@%s/consize?sslmode=disable", listener.Addr()),
		"-timeout", "75ms",
	})
	if err == nil {
		t.Fatal("stalled database connection did not time out")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("storage command exceeded bounded timeout: %s", elapsed)
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("error does not report timeout: %v", err)
	}
	if strings.Contains(err.Error(), "test-password") {
		t.Fatalf("timeout error exposed the database password: %v", err)
	}
}

func TestPostgresStorageCommandRejectsNonPositiveTimeout(t *testing.T) {
	err := runStorage(context.Background(), []string{
		"status",
		"-database-url", "postgres://consize@example.invalid/consize",
		"-timeout", "0s",
	})
	if err == nil || !strings.Contains(err.Error(), "-timeout must be greater than zero") {
		t.Fatalf("error = %v", err)
	}
}
