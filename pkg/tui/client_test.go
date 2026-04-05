package tui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/check" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"state": "ready"})
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	state, err := c.CheckAuth()
	if err != nil {
		t.Fatal(err)
	}
	if state.State != "ready" {
		t.Errorf("expected ready, got %s", state.State)
	}
}

func TestLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["password"] != "test123" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "wrong password"})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sage-auth", Value: "token123", Path: "/"})
		json.NewEncoder(w).Encode(map[string]string{"state": "ready"})
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	if err := c.Login("test123"); err != nil {
		t.Fatal(err)
	}
}

func TestRPC(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rpc" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var req rpcRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Method != "sessions.list" {
			t.Errorf("unexpected method: %s", req.Method)
		}
		json.NewEncoder(w).Encode(rpcResponse{
			Result: json.RawMessage(`[{"id":"s1","title":"Test Session"}]`),
		})
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	raw, err := c.RPC("sessions.list", map[string]string{"agent_id": "default"})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `[{"id":"s1","title":"Test Session"}]` {
		t.Errorf("unexpected result: %s", string(raw))
	}
}

func TestListAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]AgentInfo{
			{ID: "default", Name: "SageClaw", Role: "assistant"},
		})
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	agents, err := c.ListAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].ID != "default" {
		t.Errorf("unexpected agents: %+v", agents)
	}
}

func TestSendMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Method != "chat.send" {
			t.Errorf("expected chat.send, got %s", req.Method)
		}
		// Verify channel is "web" by re-marshaling Params.
		raw, _ := json.Marshal(req.Params)
		var params map[string]string
		json.Unmarshal(raw, &params)
		if params["channel"] != "web" {
			t.Errorf("expected channel web, got %s", params["channel"])
		}
		json.NewEncoder(w).Encode(rpcResponse{
			Result: json.RawMessage(`{"status":"sent"}`),
		})
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	if err := c.SendMessage("default", "tui-1", "Hello"); err != nil {
		t.Fatal(err)
	}
}

func TestRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(rpcResponse{
			Error: &rpcError{Code: -1, Message: "something went wrong"},
		})
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	_, err := c.RPC("bad.method", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "rpc bad.method: something went wrong" {
		t.Errorf("unexpected error: %v", err)
	}
}
