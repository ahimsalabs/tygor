package dev

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"tygor.dev/tygor"
)

//go:embed testdata/rawrdata
var rawrData string

type Cmd struct {
	RPCDir string `help:"Directory containing generated RPC files." default:"./src/rpc" name:"rpc-dir"`
	Port   int    `help:"Port to listen on." default:"9000" short:"p"`
	Watch  bool   `help:"Watch for file changes." short:"w"`
}

func (c *Cmd) Run() error {
	svc := &Service{
		rpcDir: c.RPCDir,
	}
	app := NewApp(svc)

	// Mount under /__tygor to avoid conflicts with user services
	mux := http.NewServeMux()
	mux.Handle("/__tygor/", http.StripPrefix("/__tygor", app.Handler()))

	addr := fmt.Sprintf("localhost:%d", c.Port)
	fmt.Printf("tygor dev listening on http://%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

// NewApp creates a tygor App with the devtools service registered.
// Used both for the runtime server and for type generation.
func NewApp(svc *Service) *tygor.App {
	app := tygor.NewApp()
	devtools := app.Service("Devtools")
	devtools.Register("GetDiscovery", tygor.Query(svc.GetDiscovery))
	devtools.Register("GetSource", tygor.Query(svc.GetSource))
	devtools.Register("GetStatus", tygor.Stream(svc.GetStatus))
	devtools.Register("UpdateStatus", tygor.Exec(svc.UpdateStatus))
	devtools.Register("Reload", tygor.Exec(svc.Reload))
	return app
}

// SetupApp returns a tygor.App for type generation.
// This is discovered by `tygor gen` via signature detection.
func SetupApp() *tygor.App {
	return NewApp(&Service{})
}

// Service implements the devtools server endpoints.
type Service struct {
	rpcDir    string
	appStatus *AppStatus // status reported by vite plugin

	// Subscriber management for streaming status updates.
	// We use channels instead of emitters because Emitter.Send()
	// is not thread-safe (uses Go iterator yield function).
	statusMu   sync.Mutex
	statusSubs map[int64]chan *GetStatusResponse
	nextSubID  int64
}

// GetDiscoveryRequest is the request for Devtools.GetDiscovery.
type GetDiscoveryRequest struct{}

// GetDiscoveryResponse returns the discovery.json content.
type GetDiscoveryResponse struct {
	// Discovery is the parsed discovery schema.
	Discovery DiscoverySchema `json:"discovery"`
}

// GetDiscovery returns the discovery.json from the rpc-dir.
func (s *Service) GetDiscovery(ctx context.Context, req *GetDiscoveryRequest) (*GetDiscoveryResponse, error) {
	path := filepath.Join(s.rpcDir, "discovery.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, tygor.NewError(tygor.CodeNotFound, "discovery.json not found - run tygor gen first")
		}
		return nil, err
	}
	var schema DiscoverySchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, tygor.NewError(tygor.CodeInternal, "failed to parse discovery.json: "+err.Error())
	}
	return &GetDiscoveryResponse{Discovery: schema}, nil
}

// GetSourceRequest is the request for Devtools.GetSource.
type GetSourceRequest struct {
	File    string `json:"file"`
	Line    int    `json:"line,omitempty"`
	Context int    `json:"context,omitempty"` // lines of context around Line (default 5)
}

// SourceLine represents a single line of source code.
type SourceLine struct {
	Num       int    `json:"num"`
	Content   string `json:"content"`
	Highlight bool   `json:"highlight,omitempty"`
}

// GetSourceResponse returns source code with context.
type GetSourceResponse struct {
	File     string       `json:"file"`
	Language string       `json:"language"`
	Lines    []SourceLine `json:"lines"`
	Context  int          `json:"context"`
}

// GetSource returns source code with context around a line.
func (s *Service) GetSource(ctx context.Context, req *GetSourceRequest) (*GetSourceResponse, error) {
	// TODO: implement source reading with security checks
	return nil, tygor.NewError(tygor.CodeNotImplemented, "not yet implemented")
}

// AppStatus represents the status of the user's app as reported by vite plugin.
type AppStatus struct {
	Status string `json:"status"` // "running", "building", "error", "starting"
	Port   int    `json:"port,omitempty"`
	Error  string `json:"error,omitempty"`
	Phase  string `json:"phase,omitempty"` // "prebuild", "build", "runtime"
}

// GetStatusRequest is the request for Devtools.GetStatus.
type GetStatusRequest struct{}

// GetStatusResponse returns the combined status in flat format for the devtools UI.
// Status is a discriminated union: "ok", "error", "reloading", "starting", "disconnected".
type GetStatusResponse struct {
	Status   string   `json:"status"`
	Port     int      `json:"port,omitempty"`
	Error    string   `json:"error,omitempty"`
	Phase    string   `json:"phase,omitempty"` // "prebuild", "build", "runtime"
	Command  *string  `json:"command"`         // null when not applicable
	Cwd      string   `json:"cwd,omitempty"`
	ExitCode *int     `json:"exitCode"`           // null when not applicable
	RawrData []string `json:"rawrData,omitempty"` // sent on initial request
}

// GetStatus streams status updates for devtools UI consumption.
// The first message always includes RawrData. Subsequent messages are sent
// when UpdateStatus is called.
func (s *Service) GetStatus(ctx context.Context, req *GetStatusRequest, stream tygor.StreamWriter[*GetStatusResponse]) error {
	// Send initial status with RawrData
	resp := s.buildStatusResponse()
	resp.RawrData = loadRawrData()
	if err := stream.Send(resp); err != nil {
		return err
	}

	// Create channel for receiving updates from other goroutines
	ch := make(chan *GetStatusResponse, 1)
	subID := s.addSubscriber(ch)
	defer s.removeSubscriber(subID)

	// Listen for updates until client disconnects
	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-ch:
			if err := stream.Send(update); err != nil {
				return err
			}
		}
	}
}

// buildStatusResponse creates a status response from current state.
// Must be called with statusMu held or after snapshotting appStatus.
func (s *Service) buildStatusResponse() *GetStatusResponse {
	s.statusMu.Lock()
	appStatus := s.appStatus
	s.statusMu.Unlock()

	resp := &GetStatusResponse{}
	if appStatus == nil {
		resp.Status = "disconnected"
	} else {
		switch appStatus.Status {
		case "running":
			resp.Status = "ok"
			resp.Port = appStatus.Port
		case "building":
			resp.Status = "reloading"
		case "error":
			resp.Status = "error"
			resp.Error = appStatus.Error
			resp.Phase = appStatus.Phase
		default:
			resp.Status = "starting"
		}
	}

	return resp
}

// addSubscriber adds a channel to the subscriber list.
func (s *Service) addSubscriber(ch chan *GetStatusResponse) int64 {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()

	if s.statusSubs == nil {
		s.statusSubs = make(map[int64]chan *GetStatusResponse)
	}

	id := s.nextSubID
	s.nextSubID++
	s.statusSubs[id] = ch
	return id
}

// removeSubscriber removes a channel from the subscriber list.
func (s *Service) removeSubscriber(id int64) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	delete(s.statusSubs, id)
}

// notifySubscribers sends the current status to all subscribers.
func (s *Service) notifySubscribers() {
	s.statusMu.Lock()
	subs := make([]chan *GetStatusResponse, 0, len(s.statusSubs))
	for _, ch := range s.statusSubs {
		subs = append(subs, ch)
	}
	s.statusMu.Unlock()

	resp := s.buildStatusResponse()
	for _, ch := range subs {
		// Non-blocking send - if channel is full, subscriber will get next update
		select {
		case ch <- resp:
		default:
		}
	}
}

// UpdateStatusRequest is sent by vite plugin to update app status.
type UpdateStatusRequest struct {
	App AppStatus `json:"app"`
}

// UpdateStatusResponse is the response for UpdateStatus.
type UpdateStatusResponse struct{}

// UpdateStatus updates the app status (called by vite plugin).
// Notifies all connected GetStatus subscribers of the change.
func (s *Service) UpdateStatus(ctx context.Context, req *UpdateStatusRequest) (*UpdateStatusResponse, error) {
	s.statusMu.Lock()
	s.appStatus = &req.App
	s.statusMu.Unlock()

	s.notifySubscribers()
	return &UpdateStatusResponse{}, nil
}

// ReloadRequest triggers a reload of discovery.json.
type ReloadRequest struct {
	Files  []string `json:"files,omitempty"`
	Reason string   `json:"reason,omitempty"` // "file_change", "manual"
}

// ReloadResponse is the response for Reload.
type ReloadResponse struct{}

// Reload triggers reloading of discovery.json (called by vite plugin on file changes).
func (s *Service) Reload(ctx context.Context, req *ReloadRequest) (*ReloadResponse, error) {
	// For now, discovery is read on each request, so no action needed.
	// Future: could notify connected clients via SSE.
	return &ReloadResponse{}, nil
}

func loadRawrData() []string {
	var data []string
	for _, line := range strings.Split(rawrData, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			data = append(data, line)
		}
	}
	return data
}
