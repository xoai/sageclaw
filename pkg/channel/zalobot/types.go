package zalobot

// APIResponse is the generic Zalo Bot API envelope.
type APIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// BotUser is the result of getMe.
type BotUser struct {
	ID            string `json:"id"`
	AccountName   string `json:"account_name"`
	AccountType   string `json:"account_type"`
	CanJoinGroups bool   `json:"can_join_groups"`
}

// Update is a single item from getUpdates or a webhook delivery.
type Update struct {
	EventName string    `json:"event_name"`
	Message   ZBMessage `json:"message"`
}

// ZBMessage is a Zalo Bot message.
type ZBMessage struct {
	MessageID string `json:"message_id"`
	Date      int64  `json:"date"`
	From      ZBFrom `json:"from"`
	Chat      ZBChat `json:"chat"`
	Text      string `json:"text,omitempty"`
	Photo     string `json:"photo,omitempty"`
	Caption   string `json:"caption,omitempty"`
	Sticker   string `json:"sticker,omitempty"`
}

// ZBFrom identifies the message sender.
type ZBFrom struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	IsBot       bool   `json:"is_bot"`
}

// ZBChat identifies the conversation.
type ZBChat struct {
	ID       string `json:"id"`
	ChatType string `json:"chat_type"` // "PRIVATE" or "GROUP"
}

// SendResult is the result of sendMessage.
type SendResult struct {
	MessageID string `json:"message_id"`
	Date      int64  `json:"date"`
}
