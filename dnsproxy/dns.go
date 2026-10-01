// Package dnsproxy serves local DNS and exchanges framed DNS on injected SSH
// streams. It never opens an upstream socket or calls the system resolver.
package dnsproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const QueryTimeout = 10 * time.Second
const AttemptTimeout = 5 * time.Second
const maxConcurrent = 64

var ErrFormat = errors.New("invalid DNS message")

type Handler func(context.Context, []byte) ([]byte, error)

func ParseQuery(wire []byte) (dnsmessage.Question, dnsmessage.Header, error) {
	var m dnsmessage.Message
	if len(wire) < 12 || len(wire) > 65535 || m.Unpack(wire) != nil || m.Response || m.OpCode != 0 || m.Truncated || len(m.Questions) != 1 || len(m.Answers) != 0 || len(m.Authorities) != 0 {
		return dnsmessage.Question{}, dnsmessage.Header{}, ErrFormat
	}
	q := m.Questions[0]
	// Zone transfers have multiple response frames and are not recursive queries.
	if q.Class != dnsmessage.ClassINET || q.Type == 251 || q.Type == 252 {
		return q, m.Header, ErrFormat
	}
	return q, m.Header, nil
}

func validateResponse(wire []byte, q dnsmessage.Question, h dnsmessage.Header) error {
	var m dnsmessage.Message
	if len(wire) > 65535 || m.Unpack(wire) != nil || !m.Response || m.Truncated || m.ID != h.ID || m.OpCode != h.OpCode || len(m.Questions) != 1 {
		return ErrFormat
	}
	got := m.Questions[0]
	if got.Type != q.Type || got.Class != q.Class || !strings.EqualFold(got.Name.String(), q.Name.String()) {
		return ErrFormat
	}
	return nil
}

// Exchange owns conn. Cancellation closes SSH channels, whose SetDeadline is
// unsupported. Valid replies are never repacked (including unknown RR types).
func Exchange(ctx context.Context, conn net.Conn, query []byte) ([]byte, error) {
	if conn == nil {
		return nil, ErrFormat
	}
	defer conn.Close()
	stop := closeOnCancel(ctx, conn)
	defer stop()
	q, h, err := ParseQuery(query)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := writeFrame(conn, query); err != nil {
		return nil, err
	}
	response, err := readFrame(conn)
	if err != nil {
		return nil, err
	}
	if err := validateResponse(response, q, h); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return response, nil
}

func readFrame(r io.Reader) ([]byte, error) {
	var prefix [2]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(prefix[:]))
	if size < 12 {
		return nil, ErrFormat
	}
	wire := make([]byte, size)
	_, err := io.ReadFull(r, wire)
	return wire, err
}

func writeFrame(w io.Writer, wire []byte) error {
	frame := make([]byte, len(wire)+2)
	binary.BigEndian.PutUint16(frame, uint16(len(wire)))
	copy(frame[2:], wire)
	for len(frame) > 0 {
		n, err := w.Write(frame)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(frame) {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func closeOnCancel(ctx context.Context, c io.Closer) func() {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { c.Close(); close(done) })
	return func() {
		if !stop() {
			<-done
		}
	}
}

type Server struct {
	tcp     net.Listener
	udp     net.PacketConn
	handle  Handler
	slots   chan struct{}
	streams chan struct{}
}

// Listen binds both transports before publishing the server. Failure releases
// the first socket, so callers can safely retry startup.
func Listen(address string, handle Handler) (*Server, error) {
	if handle == nil {
		return nil, errors.New("DNS handler is nil")
	}
	tcp, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		tcp.Close()
		return nil, err
	}
	return &Server{tcp: tcp, udp: udp, handle: handle, slots: make(chan struct{}, maxConcurrent), streams: make(chan struct{}, maxConcurrent)}, nil
}

func (s *Server) Addr() net.Addr { return s.tcp.Addr() }
func (s *Server) Close()         { s.tcp.Close(); s.udp.Close() }

// Serve returns only after every accepted query and stream has stopped.
func (s *Server) Serve(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stop := closeOnCancel(ctx, serverCloser{s})
	defer stop()
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); defer cancel(); s.serveUDP(ctx) }()
	go func() { defer workers.Done(); defer cancel(); s.serveTCP(ctx) }()
	workers.Wait()
	s.Close()
}

type serverCloser struct{ *Server }

func (c serverCloser) Close() error { c.Server.Close(); return nil }

func failure(query []byte) []byte {
	q, h, err := ParseQuery(query)
	if err != nil {
		return nil
	}
	h = dnsmessage.Header{ID: h.ID, Response: true, RecursionDesired: h.RecursionDesired, RecursionAvailable: true, CheckingDisabled: h.CheckingDisabled, RCode: dnsmessage.RCodeServerFailure}
	wire, _ := (&dnsmessage.Message{Header: h, Questions: []dnsmessage.Question{q}}).Pack()
	return wire
}

func (s *Server) answer(ctx context.Context, query []byte) []byte {
	q, h, err := ParseQuery(query)
	if err != nil {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	response, err := s.handle(bounded, query)
	if err != nil || bounded.Err() != nil || validateResponse(response, q, h) != nil {
		return failure(query)
	}
	return response
}

func (s *Server) serveUDP(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	buffer := make([]byte, 65536)
	for {
		n, peer, err := s.udp.ReadFrom(buffer)
		if err != nil {
			return
		}
		if n > 65535 || ctx.Err() != nil {
			continue
		}
		query := append([]byte(nil), buffer[:n]...)
		select {
		case s.slots <- struct{}{}:
		default:
			if reply := failure(query); reply != nil {
				_, _ = s.udp.WriteTo(reply, peer)
			}
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-s.slots }()
			response := s.answer(ctx, query)
			if response == nil || ctx.Err() != nil {
				return
			}
			if reply := udpReply(query, response); reply != nil {
				_, _ = s.udp.WriteTo(reply, peer)
			}
		}()
	}
}

func udpReply(query, response []byte) []byte {
	var m dnsmessage.Message
	if m.Unpack(query) != nil {
		return nil
	}
	limit := 512
	for _, rr := range m.Additionals {
		if rr.Header.Type == dnsmessage.TypeOPT {
			limit = max(512, min(1232, int(rr.Header.Class)))
			break
		}
	}
	if len(response) <= limit {
		return response
	}
	var reply dnsmessage.Message
	if reply.Unpack(response) != nil {
		return nil
	}
	reply.Truncated = true
	reply.AuthenticData = false
	reply.Answers, reply.Authorities, reply.Additionals = nil, nil, nil
	wire, _ := reply.Pack()
	return wire
}

func (s *Server) serveTCP(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	for {
		conn, err := s.tcp.Accept()
		if err != nil {
			return
		}
		select {
		case s.streams <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-s.streams }()
			s.stream(ctx, conn)
		}()
	}
}

func (s *Server) stream(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	stop := closeOnCancel(ctx, conn)
	defer stop()
	for ctx.Err() == nil {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
		query, err := readFrame(conn)
		if err != nil {
			return
		}
		var response []byte
		select {
		case s.slots <- struct{}{}:
			response = s.answer(ctx, query)
			<-s.slots
		default:
			response = failure(query)
		}
		if response == nil || writeFrame(conn, response) != nil {
			return
		}
	}
}
