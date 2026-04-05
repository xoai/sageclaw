package tui

// OverlayType identifies which overlay is currently active.
type OverlayType int

const (
	OverlayNone OverlayType = iota
	OverlayConsent
	OverlayAgentPicker  // M4
	OverlaySessionPicker // M4
	OverlayModelPicker  // M4
	OverlayStatus       // M5
	OverlaySettings     // M5
	OverlayHelp         // M5
)

// String returns a debug-friendly name.
func (o OverlayType) String() string {
	switch o {
	case OverlayNone:
		return "none"
	case OverlayConsent:
		return "consent"
	case OverlayAgentPicker:
		return "agent_picker"
	case OverlaySessionPicker:
		return "session_picker"
	case OverlayModelPicker:
		return "model_picker"
	case OverlayStatus:
		return "status"
	case OverlaySettings:
		return "settings"
	case OverlayHelp:
		return "help"
	default:
		return "unknown"
	}
}
