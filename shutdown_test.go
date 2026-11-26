package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// A release is installed by stopping the server, so what is in hand when that
// happens has to be finished, not cut.
func TestStoppingFinishesWhatIsInHand(t *testing.T) {
	a := &App{s: tempStore(t)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	asked, stop := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- a.serveUntil(asked, ln, 5*time.Second) }()
	answering(t, ln.Addr().String())

	// A sign-in whose form is half sent, and a message on its way out.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	form := `{"email": "nobody@larkmoor.example", "password": "wrong"}`
	fmt.Fprintf(conn, "POST /api/v1/session HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		ln.Addr(), len(form), form[:10])
	a.sending.Add(1)
	time.Sleep(100 * time.Millisecond)

	stop()
	select {
	case err := <-stopped:
		t.Fatalf("the server stopped with a request and a message still in hand: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if late, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		late.Close()
		t.Fatal("a server that was asked to stop took a new connection")
	}

	fmt.Fprint(conn, form[10:])
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the sign-in in hand wasn't answered: %v %v", res, err)
	}
	select {
	case <-stopped:
		t.Fatal("the server stopped before its mail had gone")
	case <-time.After(200 * time.Millisecond):
	}
	a.sending.Done()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server never stopped")
	}
}

func TestStoppingDoesntWaitForeverForAStream(t *testing.T) {
	a := &App{s: tempStore(t)}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	asked, stop := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- a.serveUntil(asked, ln, 200*time.Millisecond) }()
	answering(t, ln.Addr().String())

	// Like an agent's stream: a request that never finishes arriving.
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /api/v1/session HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\nemail=", ln.Addr())
	time.Sleep(100 * time.Millisecond)
	stop()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a connection that never finishes kept the server from stopping")
	}
}

// answering waits until the server takes requests, since building its routes takes a
// while and a connection it hasn't yet accepted isn't one in hand.
func answering(t *testing.T, addr string) {
	t.Helper()
	for give := time.Now().Add(10 * time.Second); time.Now().Before(give); time.Sleep(10 * time.Millisecond) {
		if res, err := http.Get("http://" + addr + "/robots.txt"); err == nil {
			res.Body.Close()
			return
		}
	}
	t.Fatal("the server never answered")
}
