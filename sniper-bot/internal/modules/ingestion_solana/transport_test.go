package ingestion_solana

import (
	"context"
	"errors"
	"testing"
)

// fakeTransport returns a deterministic event/error sequence.
type fakeTransport struct {
	mode    string
	events  []TokenEvent
	errs    []error
	idx     int
	started bool
	closed  bool
}

func (f *fakeTransport) Start(ctx context.Context) error { f.started = true; return nil }
func (f *fakeTransport) Recv(ctx context.Context) (TokenEvent, error) {
	if f.idx >= len(f.errs) {
		return TokenEvent{}, errors.New("exhausted")
	}
	e, err := f.events[f.idx], f.errs[f.idx]
	f.idx++
	return e, err
}
func (f *fakeTransport) Mode() string { return f.mode }
func (f *fakeTransport) Close() error { f.closed = true; return nil }

func TestHybridTransport_FailsOverAfterN(t *testing.T) {
	primary := &fakeTransport{
		mode:   "grpc",
		events: []TokenEvent{{}, {}, {}},
		errs:   []error{errors.New("e1"), errors.New("e2"), errors.New("e3")},
	}
	fallback := &fakeTransport{
		mode:   "rpc",
		events: []TokenEvent{{Program: "pumpfun", Signature: "abc", Slot: 42}},
		errs:   []error{nil},
	}
	h := NewHybridTransport(primary, fallback, 2)

	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First two Recv calls hit primary and error.
	if _, err := h.Recv(context.Background()); err == nil {
		t.Fatal("call 1: expected error")
	}
	if h.UsingFallback() {
		t.Fatal("must not have switched after 1 error (threshold=2)")
	}
	if _, err := h.Recv(context.Background()); err == nil {
		t.Fatal("call 2: expected error")
	}
	if !h.UsingFallback() {
		t.Fatal("must have switched after 2 consecutive errors")
	}

	// Third Recv must come from fallback.
	evt, err := h.Recv(context.Background())
	if err != nil {
		t.Fatalf("call 3 fallback: %v", err)
	}
	if evt.Program != "pumpfun" || evt.Signature != "abc" {
		t.Fatalf("fallback event mismatch: %+v", evt)
	}
	if h.Mode() != "rpc" {
		t.Fatalf("Mode after fallback: got %q want rpc", h.Mode())
	}
}

func TestHybridTransport_NoFallbackWhenDisabled(t *testing.T) {
	primary := &fakeTransport{
		mode:   "grpc",
		events: []TokenEvent{{}, {}, {}, {}, {}},
		errs:   []error{errors.New("e"), errors.New("e"), errors.New("e"), errors.New("e"), errors.New("e")},
	}
	fallback := &fakeTransport{mode: "rpc", events: []TokenEvent{{}}, errs: []error{nil}}
	h := NewHybridTransport(primary, fallback, 0) // disabled

	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for i := 0; i < 5; i++ {
		_, _ = h.Recv(context.Background())
	}
	if h.UsingFallback() {
		t.Fatal("must NOT switch when MaxErrors<=0")
	}
}

func TestHybridTransport_ResetsErrorCounterOnSuccess(t *testing.T) {
	primary := &fakeTransport{
		mode:   "grpc",
		events: []TokenEvent{{}, {Program: "raydium-v4"}, {}, {}},
		errs:   []error{errors.New("e"), nil, errors.New("e"), errors.New("e")},
	}
	fallback := &fakeTransport{mode: "rpc", events: []TokenEvent{{}}, errs: []error{nil}}
	h := NewHybridTransport(primary, fallback, 2)
	_ = h.Start(context.Background())

	_, _ = h.Recv(context.Background())                     // err 1/2
	if _, err := h.Recv(context.Background()); err != nil { // success → reset
		t.Fatalf("expected nil err, got %v", err)
	}
	_, _ = h.Recv(context.Background()) // err 1/2 again
	if h.UsingFallback() {
		t.Fatal("must NOT have switched: counter must reset on success")
	}
	_, _ = h.Recv(context.Background()) // err 2/2 → switch
	if !h.UsingFallback() {
		t.Fatal("must switch after 2 consecutive errors post-reset")
	}
}

func TestRpcAndGrpcStubsReturnNotImplemented(t *testing.T) {
	if _, err := NewRpcTransport().Recv(context.Background()); !errors.Is(err, ErrTransportNotImplemented) {
		t.Fatalf("RpcTransport.Recv: want ErrTransportNotImplemented, got %v", err)
	}
	if err := NewGrpcTransport("h:1", "tok").Start(context.Background()); !errors.Is(err, ErrTransportNotImplemented) {
		t.Fatalf("GrpcTransport.Start: want ErrTransportNotImplemented, got %v", err)
	}
}
