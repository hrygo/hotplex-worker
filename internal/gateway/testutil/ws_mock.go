package testutil

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gorilla/websocket"
)

// MockWSServer is a reusable WebSocket mock server for integration tests.
type MockWSServer struct {
	Server   *httptest.Server
	Upgrader websocket.Upgrader
	Handler  func(conn *websocket.Conn)
}

// NewMockWSServer creates a new WebSocket mock server.
// The handler function is called for each WebSocket connection in a detached goroutine
// so that the HTTP handler can return immediately and the server can close cleanly.
func NewMockWSServer(handler func(conn *websocket.Conn)) *MockWSServer {
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Detach so the HTTP handler goroutine exits immediately.
		// The httptest.Server goroutine is not blocked by the WebSocket read loop.
		go func() {
			handler(conn)
		}()
	})

	return &MockWSServer{
		Server:  httptest.NewServer(mux),
		Handler: handler,
	}
}

// Close shuts down the mock server.
func (s *MockWSServer) Close() {
	if s.Server != nil {
		s.Server.Close()
	}
}

// URL returns the WebSocket URL for connecting to the server.
func (s *MockWSServer) URL() string {
	if s.Server == nil {
		return ""
	}
	return "ws" + s.Server.URL[4:]
}

func DialAndInit(serverURL string, initEnvelope map[string]any) (*websocket.Conn, map[string]any, error) {
	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		return nil, nil, err
	}

	if err := conn.WriteJSON(initEnvelope); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	return conn, resp, nil
}
