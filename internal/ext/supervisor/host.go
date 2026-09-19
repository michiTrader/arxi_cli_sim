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
	"arxi.local/sim/internal/ext/v1"
	"arxi.local/sim/internal/ext/v2"
)

var (
	ErrHandshakeTimeout = errors.New("extension handshake timed out")
	ErrFatalProtocol    = errors.New("fatal extension protocol error")
	ErrUnexpectedExit   = errors.New("extension exited unexpectedly")
)

type Proposal struct {
	Extension, ID string
	Message       v1.Message
	V2            v2.Message
}

// Kind returns the protocol-neutral wire message kind.
func (p Proposal) Kind() string {
	if p.Message != nil {
		return p.Message.MessageType()
	}
	if p.V2 != nil {
		return p.V2.MessageType()
	}
	return ""
}

type HostConfig struct {
	Manifest                          ext.Manifest
	Granted                           ext.CapabilitySet
	HandshakeTimeout, ShutdownTimeout time.Duration
	QueueSize, StderrBytes            int
	Environment                       []string
}
type outbound struct{ event v1.Event }
type Host struct {
	cfg                HostConfig
	cmd                *exec.Cmd
	stdin              io.WriteCloser
	enc                *v1.Encoder
	queue              chan outbound
	wake               chan struct{}
	control            chan v1.Envelope
	proposals          chan Proposal
	done               chan struct{}
	closeReq           chan struct{}
	stderr             *boundedBuffer
	mu                 sync.Mutex
	err                error
	requested          ext.CapabilitySet
	subTypes           map[string]struct{}
	subAfter           int
	dropped, dropAfter int
	closeOnce          sync.Once
	v2                 *v2Session
}

func StartHost(ctx context.Context, cfg HostConfig) (*Host, error) {
	if cfg.Manifest.Protocol == v2.Protocol {
		return startV2Host(ctx, cfg)
	}
	return startV1Host(ctx, cfg)
}

func startV1Host(ctx context.Context, cfg HostConfig) (*Host, error) {
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
	h := &Host{cfg: cfg, cmd: cmd, stdin: stdin, enc: v1.NewEncoder(stdin), queue: make(chan outbound, cfg.QueueSize), wake: make(chan struct{}, 1), control: make(chan v1.Envelope, 1), proposals: make(chan Proposal, cfg.QueueSize), done: make(chan struct{}), closeReq: make(chan struct{}), stderr: newBoundedBuffer(cfg.StderrBytes), requested: ext.NewCapabilitySet(), subTypes: map[string]struct{}{}}
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
	dec := v1.NewDecoder(stdout)
	if err = h.handshake(ctx, dec); err != nil {
		_ = killProcessTree(cmd)
		_ = cmd.Wait()
		releaseProcess(cmd)
		return nil, err
	}
	go h.run(dec)
	return h, nil
}
func defaults(c *HostConfig) {
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = 5 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 2 * time.Second
	}
	if c.QueueSize <= 0 {
		c.QueueSize = 64
	}
	if c.StderrBytes <= 0 {
		c.StderrBytes = 64 << 10
	}
}
func (h *Host) handshake(ctx context.Context, dec *v1.Decoder) error {
	hello, _ := v1.Pack("", v1.Hello{Protocol: v1.Protocol, Capabilities: ext.Capabilities()})
	if err := h.enc.Encode(hello); err != nil {
		return err
	}
	type result struct {
		env v1.Envelope
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
		m, err := v1.Unpack(r.env)
		if err != nil {
			return fatal("ready", err)
		}
		ready, ok := m.(*v1.Ready)
		if !ok {
			return fatal("ready", errors.New("expected ready"))
		}
		if ready.Name != h.cfg.Manifest.Name || ready.Version != h.cfg.Manifest.Version {
			return fatal("ready", errors.New("identity mismatch"))
		}
		declared := ext.NewCapabilitySet(h.cfg.Manifest.Capabilities...)
		for _, c := range ready.Capabilities {
			if err = v1.Authorize(c, declared, h.cfg.Granted); err != nil {
				return fatal("ready", err)
			}
			h.requested[c] = struct{}{}
		}
		return nil
	}
}
func fatal(where string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrFatalProtocol, where, err)
}
func (h *Host) Proposals() <-chan Proposal { return h.proposals }
func (h *Host) Done() <-chan struct{}      { return h.done }
func (h *Host) Err() error                 { h.mu.Lock(); defer h.mu.Unlock(); return h.err }
func (h *Host) Stderr() string             { return h.stderr.String() }
func (h *Host) Invoke(id, action, args string) bool {
	if h.v2 != nil {
		env, err := v2.Pack(id, v2.InvokeAction{Action: action, Args: args})
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
	env, err := v1.Pack(id, v1.InvokeAction{Action: action, Args: args})
	if err != nil {
		return false
	}
	select {
	case <-h.done:
		return false
	case h.control <- env:
		return true
	default:
		return false
	}
}
func (h *Host) ViewResize(id string, width, height int) bool {
	if h.v2 == nil {
		return false
	}
	h.mu.Lock()
	current, ok := h.v2.panels[id]
	if !ok || current == (panelSize{width, height}) {
		h.mu.Unlock()
		return false
	}
	h.v2.panels[id] = panelSize{width, height}
	h.mu.Unlock()
	if h.v2Queue(v2.ViewResize{ID: id, Width: width, Height: height}) {
		return true
	}
	h.mu.Lock()
	if h.v2.panels[id] == (panelSize{width, height}) {
		h.v2.panels[id] = current
	}
	h.mu.Unlock()
	return false
}
func (h *Host) ViewFocus(id string) bool {
	if h.v2 == nil {
		return false
	}
	h.mu.Lock()
	_, ok := h.v2.panels[id]
	if !ok || h.v2.focused == id {
		h.mu.Unlock()
		return false
	}
	previous := h.v2.focused
	h.v2.focused = id
	h.mu.Unlock()
	if h.v2Queue(v2.ViewFocus{ID: id}) {
		return true
	}
	h.mu.Lock()
	if h.v2.focused == id {
		h.v2.focused = previous
	}
	h.mu.Unlock()
	return false
}
func (h *Host) ViewBlur(id string) bool {
	if h.v2 == nil {
		return false
	}
	h.mu.Lock()
	if h.v2.focused != id {
		h.mu.Unlock()
		return false
	}
	h.v2.focused = ""
	h.mu.Unlock()
	if h.v2Queue(v2.ViewBlur{ID: id}) {
		return true
	}
	h.mu.Lock()
	if h.v2.focused == "" {
		h.v2.focused = id
	}
	h.mu.Unlock()
	return false
}
func (h *Host) ViewInput(id string, input v2.Input) bool {
	if h.v2 == nil {
		return false
	}
	h.mu.Lock()
	size, ok := h.v2.panels[id]
	focused := h.v2.focused == id
	h.mu.Unlock()
	if !ok || !focused || input.Width != size.width || input.Height != size.height {
		return false
	}
	return h.v2Queue(v2.ViewInput{ID: id, Input: input})
}

func (h *Host) Publish(e v1.Event) bool {
	h.mu.Lock()
	if _, ok := h.requested[ext.EventsSubscribe]; !ok || e.Seq <= h.subAfter {
		h.mu.Unlock()
		return false
	}
	if len(h.subTypes) > 0 {
		if _, ok := h.subTypes[e.Type]; !ok {
			h.mu.Unlock()
			return false
		}
	}
	h.mu.Unlock()
	select {
	case <-h.done:
		return false
	default:
	}
	select {
	case h.queue <- outbound{e}:
		return true
	default:
		h.mu.Lock()
		h.dropped++
		if h.dropAfter == 0 {
			h.dropAfter = e.Seq - 1
		}
		h.mu.Unlock()
		select {
		case h.wake <- struct{}{}:
		default:
		}
		return false
	}
}
func (h *Host) run(dec *v1.Decoder) {
	read := make(chan error, 1)
	go func() { read <- h.readLoop(dec) }()
	write := make(chan error, 1)
	go func() { write <- h.writeLoop() }()
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
	if !closing {
		_ = gracefulProcess(h.cmd)
	} else {
		shutdown, _ := v1.Pack("", v1.Shutdown{})
		select {
		case h.control <- shutdown:
		default:
		}
		_ = gracefulProcess(h.cmd)
	}
	t := time.NewTimer(h.cfg.ShutdownTimeout)
	select {
	case err := <-wait:
		if !closing && result == nil {
			result = err
		}
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
func (h *Host) readLoop(dec *v1.Decoder) error {
	for {
		env, err := dec.Decode()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrUnexpectedExit
			}
			return fatal("decode", err)
		}
		msg, err := v1.Unpack(env)
		if err != nil {
			return fatal("decode", err)
		}
		switch x := msg.(type) {
		case *v1.Subscribe:
			if err = h.authorize(ext.EventsSubscribe); err != nil {
				return err
			}
			h.mu.Lock()
			h.subAfter = x.After
			h.subTypes = map[string]struct{}{}
			for _, t := range x.Types {
				h.subTypes[t] = struct{}{}
			}
			h.mu.Unlock()
		case *v1.EmitEvent:
			if err = h.authorize(ext.EventsEmit); err != nil {
				return err
			}
			if !h.emit(Proposal{Extension: h.cfg.Manifest.Name, ID: env.ID, Message: x}) {
				return ErrUnexpectedExit
			}
		case *v1.InboxAnswer:
			if err = h.authorize(ext.InboxAnswer); err != nil {
				return err
			}
			if !h.emit(Proposal{Extension: h.cfg.Manifest.Name, ID: env.ID, Message: x}) {
				return ErrUnexpectedExit
			}
		case *v1.RegisterActions:
			if err = h.authorize(ext.ActionsRegister); err != nil {
				return err
			}
			if !h.emit(Proposal{Extension: h.cfg.Manifest.Name, ID: env.ID, Message: x}) {
				return ErrUnexpectedExit
			}
		default:
			return fatal("output", fmt.Errorf("unexpected %s", env.Type))
		}
	}
}
func (h *Host) authorize(c ext.Capability) error {
	h.mu.Lock()
	_, active := h.requested[c]
	h.mu.Unlock()
	if !active {
		return fatal("authorization", fmt.Errorf("capability %q not requested", c))
	}
	return nil
}
func (h *Host) emit(p Proposal) bool {
	select {
	case h.proposals <- p:
		return true
	case <-h.closeReq:
		return false
	case <-h.done:
		return false
	}
}
func (h *Host) writeLoop() error {
	for {
		select {
		case env := <-h.control:
			if err := h.enc.Encode(env); err != nil {
				return err
			}
		case item := <-h.queue:
			env, _ := v1.Pack("", item.event)
			if err := h.enc.Encode(env); err != nil {
				return err
			}
		case <-h.wake:
			if err := h.writeGap(); err != nil {
				return err
			}
		case <-h.closeReq:
			return nil
		case <-h.done:
			return nil
		}
	}
}
func (h *Host) writeGap() error {
	h.mu.Lock()
	n, after := h.dropped, h.dropAfter
	h.dropped, h.dropAfter = 0, 0
	h.mu.Unlock()
	if n == 0 {
		return nil
	}
	env, _ := v1.Pack("", v1.EventsDropped{Count: n, AfterSeq: after})
	return h.enc.Encode(env)
}
func (h *Host) finish(err error) {
	h.mu.Lock()
	h.err = err
	h.mu.Unlock()
	close(h.done)
}
func (h *Host) Close() error { h.closeOnce.Do(func() { close(h.closeReq); <-h.done }); return nil }

type boundedBuffer struct {
	mu   sync.Mutex
	data []byte
	max  int
}

func newBoundedBuffer(n int) *boundedBuffer { return &boundedBuffer{max: n} }
func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n >= b.max {
		b.data = append(b.data[:0], p[n-b.max:]...)
		return n, nil
	}
	over := len(b.data) + n - b.max
	if over > 0 {
		b.data = append([]byte(nil), b.data[over:]...)
	}
	b.data = append(b.data, p...)
	return n, nil
}
func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte(nil), b.data...))
}
