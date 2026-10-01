package dnsproxy

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func testQuery(t *testing.T, kind dnsmessage.Type, edns uint16) []byte {
	t.Helper()
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: 1234, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName("www.example.test."), Type: kind, Class: dnsmessage.ClassINET}}}
	if edns > 0 {
		m.Additionals = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName("."), Type: dnsmessage.TypeOPT, Class: dnsmessage.Class(edns)}, Body: &dnsmessage.OPTResource{Options: []dnsmessage.Option{{Code: 65001, Data: []byte{1, 2, 3}}}}}}
	}
	wire, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func testResponse(t *testing.T, query []byte, body dnsmessage.ResourceBody, code dnsmessage.RCode) []byte {
	t.Helper()
	var m dnsmessage.Message
	if err := m.Unpack(query); err != nil {
		t.Fatal(err)
	}
	m.Response, m.RecursionAvailable, m.AuthenticData, m.RCode = true, true, true, code
	if body != nil {
		m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Type: m.Questions[0].Type, Class: dnsmessage.ClassINET, TTL: 60}, Body: body}}
	}
	wire, err := m.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func exchangeFixture(t *testing.T, query, reply []byte) ([]byte, error) {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		got, err := readFrame(server)
		if err != nil || !bytes.Equal(got, query) {
			return
		}
		_ = writeFrame(server, reply)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := Exchange(ctx, client, query)
	<-done
	return got, err
}

func TestExchangePreservesRecordsAndEDNS(t *testing.T) {
	cases := []struct {
		name string
		kind dnsmessage.Type
		body dnsmessage.ResourceBody
	}{
		{"A", dnsmessage.TypeA, &dnsmessage.AResource{A: [4]byte{192, 0, 2, 1}}},
		{"AAAA", dnsmessage.TypeAAAA, &dnsmessage.AAAAResource{AAAA: [16]byte{0x20, 1, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}}},
		{"CNAME", dnsmessage.TypeCNAME, &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("alias.test.")}},
		{"TXT", dnsmessage.TypeTXT, &dnsmessage.TXTResource{TXT: []string{"test"}}},
		{"MX", dnsmessage.TypeMX, &dnsmessage.MXResource{Pref: 10, MX: dnsmessage.MustNewName("mail.test.")}},
		{"HTTPS", dnsmessage.Type(65), &dnsmessage.UnknownResource{Type: dnsmessage.Type(65), Data: []byte{0, 1, 0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := testQuery(t, tc.kind, 1232)
			response := testResponse(t, q, tc.body, dnsmessage.RCodeSuccess)
			got, err := exchangeFixture(t, q, response)
			if err != nil || !bytes.Equal(got, response) {
				t.Fatalf("reply changed: %v", err)
			}
		})
	}
	q := testQuery(t, dnsmessage.TypeA, 0)
	negative := testResponse(t, q, nil, dnsmessage.RCodeNameError)
	got, err := exchangeFixture(t, q, negative)
	if err != nil || !bytes.Equal(got, negative) {
		t.Fatalf("negative answer changed: %v", err)
	}
}

func TestExchangeRejectsInvalidResponses(t *testing.T) {
	q := testQuery(t, dnsmessage.TypeA, 0)
	for _, change := range []func(*dnsmessage.Message){
		func(m *dnsmessage.Message) { m.ID++ }, func(m *dnsmessage.Message) { m.Response = false },
		func(m *dnsmessage.Message) { m.Truncated = true }, func(m *dnsmessage.Message) { m.OpCode = 1 },
		func(m *dnsmessage.Message) { m.Questions[0].Name = dnsmessage.MustNewName("wrong.test.") },
		func(m *dnsmessage.Message) { m.Questions[0].Type = dnsmessage.TypeAAAA },
		func(m *dnsmessage.Message) { m.Questions[0].Class = 3 },
	} {
		var m dnsmessage.Message
		_ = m.Unpack(testResponse(t, q, nil, 0))
		change(&m)
		reply, _ := m.Pack()
		if _, err := exchangeFixture(t, q, reply); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}

func TestExchangeCancellationClosesBlockedStream(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Exchange(ctx, client, testQuery(t, dnsmessage.TypeA, 0)); err == nil {
		t.Fatal("blocked write succeeded")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation did not close stream")
	}
}

func TestServerUDPTruncationAndTCPRetry(t *testing.T) {
	q := testQuery(t, dnsmessage.TypeTXT, 0)
	reply := testResponse(t, q, &dnsmessage.TXTResource{TXT: []string{string(bytes.Repeat([]byte("a"), 250)), string(bytes.Repeat([]byte("b"), 250)), string(bytes.Repeat([]byte("c"), 250))}}, 0)
	s, err := Listen("127.0.0.1:0", func(context.Context, []byte) ([]byte, error) { return reply, nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Serve(ctx); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("server did not stop")
		}
	}()
	udp, err := net.Dial("udp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(time.Second))
	_, _ = udp.Write(q)
	buf := make([]byte, 2048)
	n, err := udp.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	var m dnsmessage.Message
	if m.Unpack(buf[:n]) != nil || !m.Truncated || m.AuthenticData || n > 512 {
		t.Fatalf("bad truncated reply: %+v", m)
	}
	conn, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	// Multiple requests on one TCP connection are supported.
	for i := 0; i < 2; i++ {
		if err := writeFrame(conn, q); err != nil {
			t.Fatal(err)
		}
		got, err := readFrame(conn)
		if err != nil || !bytes.Equal(got, reply) {
			t.Fatalf("TCP retry: %v", err)
		}
	}
	edns := testQuery(t, dnsmessage.TypeTXT, 1232)
	if got := udpReply(edns, reply); !bytes.Equal(got, reply) {
		t.Fatal("EDNS budget was ignored")
	}
	large := testResponse(t, edns, &dnsmessage.TXTResource{TXT: []string{string(bytes.Repeat([]byte("x"), 250)), string(bytes.Repeat([]byte("x"), 250)), string(bytes.Repeat([]byte("x"), 250)), string(bytes.Repeat([]byte("x"), 250)), string(bytes.Repeat([]byte("x"), 250))}}, 0)
	if got := udpReply(testQuery(t, dnsmessage.TypeTXT, 4096), large); len(got) > 1232 {
		t.Fatal("UDP response exceeds safe cap")
	}
}

func TestListenFailureReleasesOtherSocket(t *testing.T) {
	occupied, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	if s, err := Listen(occupied.LocalAddr().String(), func(context.Context, []byte) ([]byte, error) { return nil, nil }); err == nil {
		s.Close()
		t.Fatal("UDP conflict accepted")
	}
	tcp, err := net.Listen("tcp", occupied.LocalAddr().String())
	if err != nil {
		t.Fatalf("TCP socket leaked: %v", err)
	}
	tcp.Close()
}

func TestServerFailureMalformedAndCapacity(t *testing.T) {
	s := &Server{handle: func(context.Context, []byte) ([]byte, error) { return nil, errors.New("no outlet") }}
	q := testQuery(t, dnsmessage.TypeA, 0)
	var m dnsmessage.Message
	if m.Unpack(s.answer(context.Background(), q)) != nil || m.RCode != dnsmessage.RCodeServerFailure {
		t.Fatal("expected SERVFAIL")
	}
	if s.answer(context.Background(), []byte{1}) != nil {
		t.Fatal("malformed query forwarded")
	}
	var calls sync.WaitGroup
	calls.Add(maxConcurrent)
	block := make(chan struct{})
	srv, err := Listen("127.0.0.1:0", func(ctx context.Context, _ []byte) ([]byte, error) {
		calls.Done()
		select {
		case <-block:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { srv.Serve(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	udp, err := net.Dial("udp", srv.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(3 * time.Second))
	for i := 0; i < maxConcurrent; i++ {
		_, _ = udp.Write(q)
	}
	ready := make(chan struct{})
	go func() { calls.Wait(); close(ready) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("queries not admitted")
	}
	_, _ = udp.Write(q)
	buf := make([]byte, 1024)
	n, err := udp.Read(buf)
	if err != nil || m.Unpack(buf[:n]) != nil || m.RCode != dnsmessage.RCodeServerFailure {
		t.Fatalf("capacity rejection: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown leaked active queries")
	}
}
