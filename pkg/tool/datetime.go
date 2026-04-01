package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// RegisterDatetime registers the datetime tool.
func RegisterDatetime(reg *Registry) {
	reg.RegisterFull("datetime", "Get the current date, time, and timezone",
		json.RawMessage(`{"type":"object","properties":{}}`),
		GroupCore, RiskSafe, "builtin", true, datetimeTool())
}

func datetimeTool() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		now := time.Now()
		zone, _ := now.Zone()

		result := fmt.Sprintf(`{"timestamp_ms": %d, "iso_8601": %q, "timezone": %q}`,
			now.UnixMilli(), now.Format(time.RFC3339), zone)

		return &canonical.ToolResult{Content: result}, nil
	}
}
