package rendezvous

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/BorisGB2001/RuConnect/internal/rendezvous/pb"
)

func relayHello(t *testing.T, addr, uuid, key string, extra []byte) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := proto.Marshal(&pb.RendezvousMessage{Union: &pb.RendezvousMessage_RequestRelay{
		RequestRelay: &pb.RequestRelay{Uuid: uuid, LicenceKey: key}}})
	var buf bytes.Buffer
	writeFrame(&buf, b)
	buf.Write(extra) // bytes sent right behind the request must survive pairing
	c.Write(buf.Bytes())
	return c
}

func TestRelayPairs(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	rl := NewRelay("KEY", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rl.Serve(ctx, ln)
	addr := ln.Addr().String()

	a := relayHello(t, addr, "u1", "KEY", []byte("early"))
	time.Sleep(50 * time.Millisecond)
	b := relayHello(t, addr, "u1", "KEY", nil)
	b.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, 5)
	if _, err := io.ReadFull(b, got); err != nil || string(got) != "early" {
		t.Fatalf("early bytes: %q %v", got, err)
	}
	b.Write([]byte("hi"))
	a.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(a, got[:2]); err != nil || string(got[:2]) != "hi" {
		t.Fatalf("b->a: %q %v", got[:2], err)
	}
	a.Close()
	b.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := b.Read(got); err == nil {
		t.Fatal("pair not torn down")
	}

	bad := relayHello(t, addr, "u2", "WRONG", nil)
	bad.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := bad.Read(got); err == nil {
		t.Fatal("wrong key accepted")
	}
}
