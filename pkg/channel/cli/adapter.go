package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
)

const (
	connIDDefault = "cli_local"
	chatID        = "cli-local"
)

// Adapter implements channel.Channel for interactive terminal chat.
type Adapter struct {
	connID string
	reader *bufio.Scanner
	writer io.Writer
	msgBus bus.MessageBus
	cancel context.CancelFunc
}

// Option configures the CLI adapter.
type Option func(*Adapter)

// WithIO overrides stdin/stdout (for testing).
func WithIO(reader io.Reader, writer io.Writer) Option {
	return func(a *Adapter) {
		a.reader = bufio.NewScanner(reader)
		a.writer = writer
	}
}

// New creates a new CLI adapter.
func New(opts ...Option) *Adapter {
	a := &Adapter{
		connID: connIDDefault,
		reader: bufio.NewScanner(os.Stdin),
		writer: os.Stdout,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *Adapter) ID() string       { return a.connID }
func (a *Adapter) Platform() string  { return "cli" }

// Start begins the interactive read loop and subscribes to outbound messages.
func (a *Adapter) Start(ctx context.Context, msgBus bus.MessageBus) error {
	a.msgBus = msgBus
	cliCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Subscribe to outbound messages for response delivery.
	msgBus.SubscribeOutbound(ctx, func(env bus.Envelope) {
		if env.ChatID == chatID || env.Channel == a.connID {
			a.printResponse(env)
		}
	})

	go a.readLoop(cliCtx)
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func (a *Adapter) readLoop(ctx context.Context) {
	fmt.Fprintf(a.writer, "SageClaw interactive mode. Type /quit to exit.\n\n")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		fmt.Fprintf(a.writer, "you> ")
		if !a.reader.Scan() {
			return // EOF or error.
		}

		input := a.reader.Text()

		// Multi-line: trailing backslash continues.
		for strings.HasSuffix(input, "\\") {
			input = input[:len(input)-1] + "\n"
			fmt.Fprintf(a.writer, "...> ")
			if !a.reader.Scan() {
				break
			}
			input += a.reader.Text()
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Local commands.
		if a.handleLocalCommand(input) {
			continue
		}

		// Publish to inbound bus.
		a.msgBus.PublishInbound(ctx, bus.Envelope{
			Channel: a.connID,
			ChatID:  chatID,
			Messages: []canonical.Message{
				{Role: "user", Content: []canonical.Content{{Type: "text", Text: input}}},
			},
		})
	}
}

func (a *Adapter) handleLocalCommand(input string) bool {
	lower := strings.ToLower(input)
	switch {
	case lower == "/quit" || lower == "/exit":
		fmt.Fprintf(a.writer, "Goodbye.\n")
		if a.cancel != nil {
			a.cancel()
		}
		os.Exit(0)
		return true
	case lower == "/clear":
		fmt.Fprintf(a.writer, "\033[2J\033[H") // ANSI clear screen.
		return true
	case lower == "/status":
		fmt.Fprintf(a.writer, "SageClaw is running.\n\n")
		return true
	}
	return false
}

func (a *Adapter) printResponse(env bus.Envelope) {
	for _, msg := range env.Messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				fmt.Fprintf(a.writer, "\nsageclaw> %s\n\n", c.Text)
			}
		}
	}
}
