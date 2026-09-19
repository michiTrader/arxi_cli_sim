package supervisor

import (
	"context"
	"os"
	"testing"
	"time"

	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ext/v2"
)

func TestV2HelperProcess(t *testing.T) {
	if os.Getenv("ARXI_V2_HELPER") == "" {
		return
	}
	dec, enc := v2.NewDecoder(os.Stdin), v2.NewEncoder(os.Stdout)
	env, err := dec.Decode()
	if err != nil {
		os.Exit(2)
	}
	msg, err := v2.Unpack(env)
	if err != nil {
		os.Exit(3)
	}
	if msg.(*v2.Hello).Protocol != v2.Protocol {
		os.Exit(4)
	}
	caps := []ext.Capability{ext.PanelRender}
	ready, _ := v2.Pack("", v2.Ready{Protocol: v2.Protocol, Name: "helper", Version: "1", Capabilities: caps})
	_ = enc.Encode(ready)
	view, _ := v2.Pack("u1", v2.ViewUpdate{ID: "main", Width: 10, Height: 2, Rows: []v2.Row{{ID: "r", Spans: []v2.Span{{Text: "first", Role: v2.RoleText}}}}})
	_ = enc.Encode(view)
	for {
		env, err = dec.Decode()
		if err != nil {
			return
		}
		msg, err = v2.Unpack(env)
		if err != nil {
			os.Exit(5)
		}
		switch x := msg.(type) {
		case *v2.ViewResize:
			if x.ID != "main" || x.Width != 20 {
				os.Exit(6)
			}
		case *v2.ViewFocus:
		case *v2.ViewInput:
			if x.ID != "main" || x.Input.Kind != "key" {
				os.Exit(7)
			}
			os.Exit(0)
		case *v2.Shutdown:
			return
		default:
			os.Exit(8)
		}
	}
}

func v2HelperConfig() HostConfig {
	caps := []ext.Capability{ext.PanelRender}
	return HostConfig{Manifest: ext.Manifest{Name: "helper", Version: "1", Protocol: v2.Protocol, Executable: os.Args[0], Args: []string{"-test.run=^TestV2HelperProcess$"}, Capabilities: caps}, Granted: ext.NewCapabilitySet(caps...), Environment: []string{"ARXI_V2_HELPER=1"}, HandshakeTimeout: 2 * time.Second, ShutdownTimeout: time.Second, QueueSize: 4}
}

func TestV2ViewAndHostControls(t *testing.T) {
	h, err := StartHost(context.Background(), v2HelperConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	select {
	case p := <-h.Proposals():
		view, ok := p.V2.(*v2.ViewUpdate)
		if !ok || p.Extension != "helper" || p.ID != "u1" || view.Rows[0].Spans[0].Text != "first" {
			t.Fatalf("proposal %#v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("view timeout")
	}
	if h.ViewResize("missing", 20, 2) || !h.ViewResize("main", 20, 2) || h.ViewResize("main", 20, 2) {
		t.Fatal("resize registration/deduplication failed")
	}
	if !h.ViewFocus("main") {
		t.Fatal("focus rejected")
	}
	if !h.ViewInput("main", v2.Input{Kind: "key", Key: "enter", Width: 20, Height: 2}) {
		t.Fatal("input rejected")
	}
	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("control timeout")
	}
}

func TestV2PanelRequiresRequestedCapability(t *testing.T) {
	cfg := v2HelperConfig()
	cfg.Manifest.Capabilities = nil
	cfg.Granted = ext.NewCapabilitySet()
	if _, err := StartHost(context.Background(), cfg); err == nil {
		t.Fatal("expected ready authorization failure")
	}
}
