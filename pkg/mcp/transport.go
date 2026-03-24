package mcp

import "context"

// Transport abstracts the communication layer for MCP client connections.
type Transport interface {
	// Connect establishes the connection and performs the MCP initialize handshake.
	Connect(ctx context.Context) error

	// Call sends a JSON-RPC request and waits for the response.
	Call(ctx context.Context, method string, params any) (*JSONRPCResponse, error)

	// Close shuts down the connection.
	Close() error

	// Healthy returns true if the connection is alive.
	Healthy() bool
}
