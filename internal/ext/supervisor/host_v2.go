package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ext/v2"
)

type panelSize struct{ width, height int }

type v2Session struct {
	enc      *v2.Encoder
	control  chan v2.Envelope
	visual   chan Proposal
	visualMu sync.Mutex
	latest   map[string]Proposal
	panels   map[string]panelSize
	focused  string
}

func startV2Host(ctx context.Context, cfg HostConfig) (*Host, error) {
	defaults(&cfg)
	if err := ext.ValidateManifest(cfg.Manifest); err != nil {
		return nil, err
	}
	cmd := exec.Command(cfg.Manifest.Executable, cfg.Manifest.Args...)
	cmd.Env = append([]string(nil), cfg.Environment...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	s := &v2Session{enc: v2.NewEncoder(stdin), control: make(chan v2.Envelope, cfg.QueueSize), visual: make(chan Proposal, 1), latest: map[string]Proposal{}, panels: map[string]panelSize{}}
	h := &Host{cfg: cfg, cmd: cmd, stdin: stdin, queue: make(chan outbound, cfg.QueueSize), wake: make(chan struct{}, 1), proposals: make(chan Proposal, cfg.QueueSize), done: make(chan struct{}), closeReq: make(chan struct{}), stderr: newBoundedBuffer(cfg.StderrBytes), requested: ext.NewCapabilitySet(), subTypes: map[string]struct{}{}, v2: s}
	cmd.Stderr = h.stderr
	if err = prepareProcess(cmd); err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, err
	}
	if err = attachProcess(cmd); err != nil {
		_ = killProcessTree(cmd)
		_ = cmd.Wait()
		return nil, err
	}
	dec := v2.NewDecoder(stdout)
	if err = h.v2Handshake(ctx, dec); err != nil {
		_ = killProcessTree(cmd)
		_ = cmd.Wait()
		releaseProcess(cmd)
		return nil, err
	}
	go h.v2Run(dec)
	return h, nil
}

func (h *Host) v2Handshake(ctx context.Context, dec *v2.Decoder) error {
	hello, _ := v2.Pack("", v2.Hello{Protocol: v2.Protocol, Capabilities: ext.CapabilitiesFor(v2.Protocol)})
	if err := h.v2.enc.Encode(hello); err != nil {
		return err
	}
	type result struct {
		env v2.Envelope
		err error
	}
	ch := make(chan result, 1)
	go func() { e, err := dec.Decode(); ch <- result{e, err} }()
	t := time.NewTimer(h.cfg.HandshakeTimeout)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return ErrHandshakeTimeout
	case r := <-ch:
		if r.err != nil {
			return fatal("ready", r.err)
		}
		msg, err := v2.Unpack(r.env)
		if err != nil {
			return fatal("ready", err)
		}
		ready, ok := msg.(*v2.Ready)
		if !ok {
			return fatal("ready", errors.New("expected ready"))
		}
		if ready.Name != h.cfg.Manifest.Name || ready.Version != h.cfg.Manifest.Version {
			return fatal("ready", errors.New("identity mismatch"))
		}
		declared := ext.NewCapabilitySet(h.cfg.Manifest.Capabilities...)
		for _, c := range ready.Capabilities {
			if err := v2.Authorize(c, declared, h.cfg.Granted); err != nil {
				return fatal("ready", err)
			}
			h.requested[c] = struct{}{}
		}
		return nil
	}
}

func (h *Host) v2Run(dec *v2.Decoder) {
	read := make(chan error, 1)
	go func() { read <- h.v2ReadLoop(dec) }()
	write := make(chan error, 1)
	go func() { write <- h.v2WriteLoop() }()
	go h.v2VisualLoop()
	wait := make(chan error, 1)
	go func() { wait <- h.cmd.Wait() }()
	var result error
	closing := false
	select {
	case result = <-read:
	case result = <-write:
	case result = <-wait:
		if result == nil {
			result = ErrUnexpectedExit
		}
		releaseProcess(h.cmd)
		h.finish(result)
		return
	case <-h.closeReq:
		closing = true
	}
	if closing {
		h.v2Queue(v2.Shutdown{})
	}
	_ = gracefulProcess(h.cmd)
	t := time.NewTimer(h.cfg.ShutdownTimeout)
	select {
	case <-wait:
	case <-t.C:
		_ = killProcessTree(h.cmd)
		<-wait
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	releaseProcess(h.cmd)
	_ = h.stdin.Close()
	h.finish(result)
}

func (h *Host) v2ReadLoop(dec *v2.Decoder) error {
	for {
		env, err := dec.Decode()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrUnexpectedExit
			}
			return fatal("decode", err)
		}
		msg, err := v2.Unpack(env)
		if err != nil {
			return fatal("decode", err)
		}
		if !v2.FromExtension(msg) {
			return fatal("output", fmt.Errorf("unexpected %s", env.Type))
		}
		switch x := msg.(type) {
		case *v2.Subscribe:
			if err = h.authorize(ext.EventsSubscribe); err != nil {
				return err
			}
			h.mu.Lock()
			h.subAfter = x.After
			h.subTypes = map[string]struct{}{}
			for _, typ := range x.Types {
				h.subTypes[typ] = struct{}{}
			}
			h.mu.Unlock()
		case *v2.EmitEvent:
			if err = h.authorize(ext.EventsEmit); err != nil {
				return err
			}
			if !h.emit(Proposal{Extension: h.cfg.Manifest.Name, ID: env.ID, V2: x}) {
				return ErrUnexpectedExit
			}
		case *v2.InboxAnswer:
			if err = h.authorize(ext.InboxAnswer); err != nil {
				return err
			}
			if !h.emit(Proposal{Extension: h.cfg.Manifest.Name, ID: env.ID, V2: x}) {
				return ErrUnexpectedExit
			}
		case *v2.RegisterActions:
			if err = h.authorize(ext.ActionsRegister); err != nil {
				return err
			}
			if !h.emit(Proposal{Extension: h.cfg.Manifest.Name, ID: env.ID, V2: x}) {
				return ErrUnexpectedExit
			}
		case *v2.ViewUpdate:
			if err = h.authorize(ext.PanelRender); err != nil {
				return err
			}
			h.v2PanelUpdate(env.ID, x)
		case *v2.ViewClose:
			if err = h.authorize(ext.PanelRender); err != nil {
				return err
			}
			h.mu.Lock()
			_, ok := h.v2.panels[x.ID]
			if ok {
				delete(h.v2.panels, x.ID)
				if h.v2.focused == x.ID {
					h.v2.focused = ""
				}
			}
			h.mu.Unlock()
			if !ok {
				return fatal("view.close", errors.New("panel is not registered"))
			}
			h.v2Visual(Proposal{Extension: h.cfg.Manifest.Name, ID: env.ID, V2: x}, x.ID)
		default:
			return fatal("output", fmt.Errorf("unexpected %s", env.Type))
		}
	}
}

func (h *Host) v2PanelUpdate(id string, view *v2.ViewUpdate) {
	copyView := *view
	copyView.Rows = append([]v2.Row(nil), view.Rows...)
	for i := range copyView.Rows {
		copyView.Rows[i].Spans = append([]v2.Span(nil), copyView.Rows[i].Spans...)
	}
	h.mu.Lock()
	h.v2.panels[view.ID] = panelSize{view.Width, view.Height}
	h.mu.Unlock()
	h.v2Visual(Proposal{Extension: h.cfg.Manifest.Name, ID: id, V2: &copyView}, view.ID)
}

func (h *Host) v2Visual(p Proposal, panel string) {
	h.v2.visualMu.Lock()
	h.v2.latest[panel] = p
	h.v2.visualMu.Unlock()
	select {
	case h.v2.visual <- Proposal{}:
	default:
	}
}
func (h *Host) v2VisualLoop() {
	for {
		select {
		case <-h.v2.visual:
			for {
				h.v2.visualMu.Lock()
				var key string
				var p Proposal
				for key, p = range h.v2.latest {
					break
				}
				if key != "" {
					delete(h.v2.latest, key)
				}
				h.v2.visualMu.Unlock()
				if key == "" {
					break
				}
				select {
				case h.proposals <- p:
				case <-h.closeReq:
					return
				case <-h.done:
					return
				}
			}
		case <-h.closeReq:
			return
		case <-h.done:
			return
		}
	}
}

func (h *Host) v2WriteLoop() error {
	for {
		select {
		case env := <-h.v2.control:
			if err := h.v2.enc.Encode(env); err != nil {
				return err
			}
		case item := <-h.queue:
			env, _ := v2.Pack("", v2.Event{Seq: item.event.Seq, Type: item.event.Type, Event: item.event.Event})
			if err := h.v2.enc.Encode(env); err != nil {
				return err
			}
		case <-h.wake:
			if err := h.v2WriteGap(); err != nil {
				return err
			}
		case <-h.closeReq:
			return nil
		case <-h.done:
			return nil
		}
	}
}
func (h *Host) v2WriteGap() error {
	h.mu.Lock()
	n, after := h.dropped, h.dropAfter
	h.dropped, h.dropAfter = 0, 0
	h.mu.Unlock()
	if n == 0 {
		return nil
	}
	env, _ := v2.Pack("", v2.EventsDropped{Count: n, AfterSeq: after})
	return h.v2.enc.Encode(env)
}
func (h *Host) v2Queue(msg v2.Message) bool {
	if h.v2 == nil || !v2.FromHost(msg) {
		return false
	}
	env, err := v2.Pack("", msg)
	if err != nil {
		return false
	}
	select {
	case <-h.done:
		return false
	case h.v2.control <- env:
		return true
	default:
		return false
	}
}
