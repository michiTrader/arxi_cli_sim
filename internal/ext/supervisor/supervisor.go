package supervisor

import (
	"context"
	"errors"
	"sync"
	"time"

	"arxi.local/sim/internal/ext/v1"
	"arxi.local/sim/internal/ext/v2"
)

type SupervisorConfig struct {
	Host                       HostConfig
	MaxRestarts                int
	InitialBackoff, MaxBackoff time.Duration
}
type Supervisor struct {
	cfg       SupervisorConfig
	ctx       context.Context
	cancel    context.CancelFunc
	proposals chan Proposal
	done      chan struct{}
	mu        sync.Mutex
	host      *Host
	err       error
	closeOnce sync.Once
}

func StartSupervisor(ctx context.Context, cfg SupervisorConfig) *Supervisor {
	if cfg.MaxRestarts < 0 {
		cfg.MaxRestarts = 0
	}
	if cfg.InitialBackoff <= 0 {
		cfg.InitialBackoff = 100 * time.Millisecond
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 5 * time.Second
	}
	child, cancel := context.WithCancel(ctx)
	n := cfg.Host.QueueSize
	if n < 1 {
		n = 1
	}
	s := &Supervisor{cfg: cfg, ctx: child, cancel: cancel, proposals: make(chan Proposal, n), done: make(chan struct{})}
	go s.run()
	return s
}
func (s *Supervisor) Proposals() <-chan Proposal { return s.proposals }
func (s *Supervisor) Done() <-chan struct{}      { return s.done }
func (s *Supervisor) Err() error                 { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Supervisor) Invoke(id, action, args string) bool {
	s.mu.Lock()
	h := s.host
	s.mu.Unlock()
	return h != nil && h.Invoke(id, action, args)
}
func (s *Supervisor) Publish(e v1.Event) bool {
	s.mu.Lock()
	h := s.host
	s.mu.Unlock()
	return h != nil && h.Publish(e)
}
func (s *Supervisor) ViewResize(id string, width, height int) bool {
	s.mu.Lock()
	h := s.host
	s.mu.Unlock()
	return h != nil && h.ViewResize(id, width, height)
}
func (s *Supervisor) ViewFocus(id string) bool {
	s.mu.Lock()
	h := s.host
	s.mu.Unlock()
	return h != nil && h.ViewFocus(id)
}
func (s *Supervisor) ViewBlur(id string) bool {
	s.mu.Lock()
	h := s.host
	s.mu.Unlock()
	return h != nil && h.ViewBlur(id)
}
func (s *Supervisor) ViewInput(id string, input v2.Input) bool {
	s.mu.Lock()
	h := s.host
	s.mu.Unlock()
	return h != nil && h.ViewInput(id, input)
}
func (s *Supervisor) run() {
	defer close(s.done)
	defer close(s.proposals)
	backoff := s.cfg.InitialBackoff
	for attempt := 0; ; attempt++ {
		h, err := StartHost(s.ctx, s.cfg.Host)
		if err == nil {
			s.mu.Lock()
			s.host = h
			s.mu.Unlock()
			for {
				select {
				case p := <-h.Proposals():
					select {
					case s.proposals <- p:
					case <-h.Done():
						err = h.Err()
						goto ended
					case <-s.ctx.Done():
						_ = h.Close()
						return
					}
				case <-h.Done():
					err = h.Err()
					goto ended
				case <-s.ctx.Done():
					_ = h.Close()
					return
				}
			}
		}
	ended:
		s.mu.Lock()
		if s.host == h {
			s.host = nil
		}
		s.mu.Unlock()
		if s.ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrFatalProtocol) || attempt >= s.cfg.MaxRestarts {
			s.mu.Lock()
			s.err = err
			s.mu.Unlock()
			return
		}
		t := time.NewTimer(backoff)
		select {
		case <-t.C:
		case <-s.ctx.Done():
			t.Stop()
			return
		}
		backoff *= 2
		if backoff > s.cfg.MaxBackoff {
			backoff = s.cfg.MaxBackoff
		}
	}
}
func (s *Supervisor) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.mu.Lock()
		h := s.host
		s.mu.Unlock()
		if h != nil {
			_ = h.Close()
		}
		<-s.done
	})
	return nil
}
