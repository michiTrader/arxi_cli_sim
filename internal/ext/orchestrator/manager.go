package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"arxi.local/sim/internal/app"
	"arxi.local/sim/internal/config"
	"arxi.local/sim/internal/ext"
	"arxi.local/sim/internal/ext/supervisor"
	"arxi.local/sim/internal/ext/v1"
	"arxi.local/sim/internal/ext/v2"
	"arxi.local/sim/internal/ext/viewmodel"
)

type Message struct {
	Extension  string
	Emit       *app.ExtensionEvent
	Answer     *app.ExtensionAnswer
	Actions    []app.ExtensionAction
	View       *viewmodel.View
	ViewClosed string
	Removed    bool
	Notice     string
}

type Manager struct {
	ctx        context.Context
	controller *config.Controller
	env        []string
	mu         sync.Mutex
	entries    map[string]*entry
	messages   chan app.ExtensionMessage
	closed     bool
	nextID     uint64
}

type entry struct {
	manifest ext.Manifest
	grant    ext.CapabilitySet
	identity string
	pending  bool
	verified bool
	rejected bool
	actions  map[string]struct{}
	sup      *supervisor.Supervisor
}

func New(ctx context.Context, controller *config.Controller, environment []string) (*Manager, error) {
	m := &Manager{ctx: ctx, controller: controller, env: append([]string(nil), environment...), entries: map[string]*entry{}, messages: make(chan app.ExtensionMessage, 64)}
	if controller == nil {
		return m, nil
	}
	for name, configured := range controller.Extensions() {
		if !configured.Enabled {
			continue
		}
		manifest, err := ext.LoadManifest(configured.Manifest)
		if err != nil {
			return nil, err
		}
		packageDigest := ""
		digestMatches := true
		if configured.PackageDigest != "" {
			packageDigest, err = ext.TreeDigest(filepath.Dir(configured.Manifest))
			if err != nil {
				return nil, err
			}
			digestMatches = packageDigest == configured.PackageDigest
		}
		id := ext.Identity(manifest, packageDigest)
		manifest.Executable = resolveExecutable(configured.Manifest, manifest.Executable)
		declared := ext.NewCapabilitySet(manifest.Capabilities...)
		pending := !digestMatches || configured.Identity != id || !configured.Allow.Equal(declared)
		m.entries[name] = &entry{manifest: manifest, grant: configured.Allow, identity: id, pending: pending, verified: digestMatches, actions: map[string]struct{}{}}
		if !pending {
			m.start(name)
		}
	}
	return m, nil
}

func resolveExecutable(manifestPath, executable string) string {
	if filepath.IsAbs(executable) || (!strings.ContainsRune(executable, '/') && !strings.ContainsRune(executable, '\\')) {
		return executable
	}
	return filepath.Clean(filepath.Join(filepath.Dir(manifestPath), executable))
}

func (m *Manager) Pending() []app.ExtensionConsent {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []app.ExtensionConsent
	for _, e := range m.entries {
		if !e.pending || e.rejected {
			continue
		}
		caps := make([]string, len(e.manifest.Capabilities))
		for i, c := range e.manifest.Capabilities {
			caps[i] = string(c)
		}
		sort.Strings(caps)
		out = append(out, app.ExtensionConsent{Name: e.manifest.Name, Version: e.manifest.Version, Executable: e.manifest.Executable, Args: append([]string(nil), e.manifest.Args...), Capabilities: caps, Identity: e.identity})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) Grant(name string) error {
	m.mu.Lock()
	e := m.entries[name]
	m.mu.Unlock()
	if e == nil || e.rejected || !e.verified {
		return errors.New("extension package is unavailable or does not match its managed digest")
	}
	grant := ext.NewCapabilitySet(e.manifest.Capabilities...)
	if err := m.controller.SetExtensionConsent(name, grant, e.identity); err != nil {
		return err
	}
	m.mu.Lock()
	e.grant, e.pending = grant, false
	m.mu.Unlock()
	m.start(name)
	return nil
}
func (m *Manager) Reject(name string) {
	m.mu.Lock()
	if e := m.entries[name]; e != nil {
		e.rejected, e.pending = true, false
	}
	m.mu.Unlock()
}
func (m *Manager) Messages() <-chan app.ExtensionMessage { return m.messages }
func (m *Manager) ViewResize(extension, panel string, width, height int) bool {
	m.mu.Lock()
	e := m.entries[extension]
	var s *supervisor.Supervisor
	if e != nil {
		s = e.sup
	}
	m.mu.Unlock()
	return s != nil && s.ViewResize(panel, width, height)
}
func (m *Manager) ViewFocus(extension, panel string) bool {
	m.mu.Lock()
	e := m.entries[extension]
	var s *supervisor.Supervisor
	if e != nil {
		s = e.sup
	}
	m.mu.Unlock()
	return s != nil && s.ViewFocus(panel)
}
func (m *Manager) ViewBlur(extension, panel string) bool {
	m.mu.Lock()
	e := m.entries[extension]
	var s *supervisor.Supervisor
	if e != nil {
		s = e.sup
	}
	m.mu.Unlock()
	return s != nil && s.ViewBlur(panel)
}
func (m *Manager) ViewInput(extension, panel string, input viewmodel.Input) bool {
	m.mu.Lock()
	e := m.entries[extension]
	var s *supervisor.Supervisor
	if e != nil {
		s = e.sup
	}
	m.mu.Unlock()
	return s != nil && s.ViewInput(panel, v2.Input{Kind: input.Kind, Key: input.Key, Action: input.Action, Text: input.Text, X: input.X, Y: input.Y, DX: input.DX, DY: input.DY, Width: input.Width, Height: input.Height})
}
func (m *Manager) Publish(e app.Event) {
	wire := v1.Event{Seq: e.Seq, Type: e.Type, Event: append(json.RawMessage(nil), e.JSON...)}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.entries {
		if x.sup != nil {
			x.sup.Publish(wire)
		}
	}
}
func (m *Manager) Invoke(extension, action, args string) error {
	m.mu.Lock()
	e := m.entries[extension]
	if e == nil || e.sup == nil || e.rejected || e.pending {
		m.mu.Unlock()
		return errors.New("extension is unavailable")
	}
	if action == "" {
		parts := strings.Fields(args)
		if len(parts) == 0 {
			m.mu.Unlock()
			return errors.New("slash invocation requires an action name")
		}
		action = parts[0]
		args = strings.TrimSpace(strings.TrimPrefix(args, action))
	}
	if _, registered := e.actions[action]; !registered {
		m.mu.Unlock()
		return errors.New("extension action is unavailable")
	}
	m.nextID++
	id := "invoke-" + strconv.FormatUint(m.nextID, 10)
	s := e.sup
	m.mu.Unlock()
	if !s.Invoke(id, action, args) {
		return errors.New("extension invocation queue is full")
	}
	return nil
}

func (m *Manager) start(name string) {
	m.mu.Lock()
	e := m.entries[name]
	if e == nil || e.sup != nil || e.rejected || e.pending {
		m.mu.Unlock()
		return
	}
	e.sup = supervisor.StartSupervisor(m.ctx, supervisor.SupervisorConfig{Host: supervisor.HostConfig{Manifest: e.manifest, Granted: e.grant, Environment: m.env}})
	s := e.sup
	m.mu.Unlock()
	go m.forward(name, s)
}

func (m *Manager) forward(name string, s *supervisor.Supervisor) {
	for p := range s.Proposals() {
		message := app.ExtensionMessage{Extension: name}
		emitApp := true
		switch x := p.Message.(type) {
		case *v1.EmitEvent:
			message.Emit = &app.ExtensionEvent{Type: x.Type, Scope: x.Scope, Payload: x.Payload}
		case *v1.InboxAnswer:
			message.Answer = &app.ExtensionAnswer{InboxID: x.InboxID, Kind: x.Kind, Answer: x.Answer}
		case *v1.RegisterActions:
			m.registerActions(name, s, x.Actions)
			for _, a := range x.Actions {
				message.Actions = append(message.Actions, app.ExtensionAction{Name: a.Name, Description: a.Description})
			}
		}
		switch x := p.V2.(type) {
		case *v2.EmitEvent:
			message.Emit = &app.ExtensionEvent{Type: x.Type, Scope: x.Scope, Payload: x.Payload}
		case *v2.InboxAnswer:
			message.Answer = &app.ExtensionAnswer{InboxID: x.InboxID, Kind: x.Kind, Answer: x.Answer}
		case *v2.RegisterActions:
			actions := make([]v1.Action, len(x.Actions))
			for i, a := range x.Actions {
				actions[i] = v1.Action{Name: a.Name, Description: a.Description}
				message.Actions = append(message.Actions, app.ExtensionAction{Name: a.Name, Description: a.Description})
			}
			m.registerActions(name, s, actions)
		case *v2.ViewUpdate:
			message.View = translateView(x)
		case *v2.ViewClose:
			message.ViewClosed = x.ID
		}
		if emitApp {
			m.messages <- message
		}
	}
	m.messages <- app.ExtensionMessage{Extension: name, Removed: true}
	m.mu.Lock()
	if e := m.entries[name]; e != nil && e.sup == s {
		e.sup = nil
		e.actions = map[string]struct{}{}
	}
	m.mu.Unlock()
}

func (m *Manager) registerActions(name string, s *supervisor.Supervisor, actions []v1.Action) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[name]; e != nil && e.sup == s {
		for _, a := range actions {
			e.actions[a.Name] = struct{}{}
		}
	}
}
func translateView(x *v2.ViewUpdate) *viewmodel.View {
	v := &viewmodel.View{ID: x.ID, Width: x.Width, Height: x.Height, Rows: make([]viewmodel.Row, len(x.Rows))}
	for i, row := range x.Rows {
		v.Rows[i] = viewmodel.Row{ID: row.ID, Spans: make([]viewmodel.Span, len(row.Spans))}
		for j, span := range row.Spans {
			v.Rows[i].Spans[j] = viewmodel.Span{Text: span.Text, Role: viewmodel.Role(span.Role)}
		}
	}
	return v
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	var supervisors []*supervisor.Supervisor
	for _, e := range m.entries {
		if e.sup != nil {
			supervisors = append(supervisors, e.sup)
		}
	}
	m.mu.Unlock()
	for _, s := range supervisors {
		_ = s.Close()
	}
	return nil
}

// MinimalEnvironment supplies only process essentials, never the complete parent environment.
func MinimalEnvironment() []string {
	keys := []string{"PATH", "SystemRoot", "WINDIR", "HOME", "TMPDIR", "TEMP", "TMP"}
	var out []string
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+value)
		}
	}
	return out
}
