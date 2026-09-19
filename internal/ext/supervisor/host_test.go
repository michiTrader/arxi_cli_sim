package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ext/v1"
)

func TestHelperProcess(t *testing.T) {
	if os.Getenv("ARXI_EXT_HELPER") == "" {
		return
	}
	dec := v1.NewDecoder(os.Stdin)
	enc := v1.NewEncoder(os.Stdout)
	env, err := dec.Decode()
	if err != nil {
		os.Exit(2)
	}
	msg, err := v1.Unpack(env)
	if err != nil {
		os.Exit(3)
	}
	hello := msg.(*v1.Hello)
	if len(hello.Capabilities) != len(ext.Capabilities()) {
		os.Exit(4)
	}
	caps := []ext.Capability{}
	if os.Getenv("ARXI_CAP") != "" {
		caps = []ext.Capability{ext.Capability(os.Getenv("ARXI_CAP"))}
	}
	ready, _ := v1.Pack("", v1.Ready{Protocol: v1.Protocol, Name: "helper", Version: "1", Capabilities: caps})
	_ = enc.Encode(ready)
	if os.Getenv("ARXI_MODE") == "malformed" {
		_, _ = os.Stdout.WriteString("not-json\n")
		select {}
	}
	if os.Getenv("ARXI_MODE") == "proposal" {
		p, _ := v1.Pack("p1", v1.EmitEvent{Type: "test", Scope: "team", Payload: json.RawMessage(`{"x":1}`)})
		_ = enc.Encode(p)
	}
	for {
		env, err = dec.Decode()
		if err != nil {
			return
		}
		if env.Type == v1.TypeActionInvoke {
			if os.Getenv("ARXI_MODE") == "invoke" {
				msg, err := v1.Unpack(env)
				if err != nil {
					os.Exit(5)
				}
				invoke := msg.(*v1.InvokeAction)
				if env.ID != "call-1" || invoke.Action != "refresh" || invoke.Args != "now" {
					os.Exit(6)
				}
				os.Exit(0)
			}
		}
		if env.Type == v1.TypeShutdown {
			return
		}
	}
}

func helperConfig(mode string, cap ext.Capability) HostConfig {
	env := []string{"ARXI_EXT_HELPER=1", "ARXI_MODE=" + mode}
	if cap != "" {
		env = append(env, "ARXI_CAP="+string(cap))
	}
	caps := []ext.Capability{}
	if cap != "" {
		caps = []ext.Capability{cap}
	}
	return HostConfig{Manifest: ext.Manifest{Name: "helper", Version: "1", Protocol: v1.Protocol, Executable: os.Args[0], Args: []string{"-test.run=^TestHelperProcess$"}, Capabilities: caps}, Granted: ext.NewCapabilitySet(caps...), Environment: env, HandshakeTimeout: 2 * time.Second, ShutdownTimeout: time.Second, QueueSize: 2, StderrBytes: 8}
}
func TestHostInvokesActionWithCorrelationID(t *testing.T) {
	h, err := StartHost(context.Background(), helperConfig("invoke", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !h.Invoke("call-1", "refresh", "now") {
		t.Fatal("Invoke rejected available control queue")
	}
	select {
	case <-h.Done():
		if err := h.Err(); err != nil && !errors.Is(err, ErrUnexpectedExit) {
			t.Fatalf("host err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("invoke timeout")
	}
}

func TestHostProposalAndIdempotentClose(t *testing.T) {
	h, err := StartHost(context.Background(), helperConfig("proposal", ext.EventsEmit))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-h.Proposals():
		if p.ID != "p1" {
			t.Fatalf("id=%q", p.ID)
		}
		if _, ok := p.Message.(*v1.EmitEvent); !ok {
			t.Fatalf("message %T", p.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("proposal timeout")
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestFatalProtocolDisablesRestart(t *testing.T) {
	s := StartSupervisor(context.Background(), SupervisorConfig{Host: helperConfig("malformed", ""), MaxRestarts: 3, InitialBackoff: time.Millisecond})
	defer s.Close()
	select {
	case <-s.Done():
		if !errors.Is(s.Err(), ErrFatalProtocol) {
			t.Fatalf("err=%v", s.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor timeout")
	}
}
func TestBoundedBuffer(t *testing.T) {
	b := newBoundedBuffer(4)
	_, _ = b.Write([]byte("abcdef"))
	if got := b.String(); got != "cdef" {
		t.Fatalf("got %q", got)
	}
}
