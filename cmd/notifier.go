package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"sync"

	"github.com/NicoNex/echotron/v3"
)

// AdminNotifier is a writer that sends error logs to Telegram admins
type AdminNotifier struct {
	api      echotron.API
	mu       sync.Mutex
	adminIDs map[int64]bool
	writer   io.Writer // Original writer to pass logs through
}

func NewAdminNotifier(api echotron.API, adminIDs map[int64]bool, writer io.Writer) *AdminNotifier {
	return &AdminNotifier{
		api:      api,
		adminIDs: adminIDs,
		writer:   writer,
	}
}

func (n *AdminNotifier) Write(p []byte) (int, error) {
	// Parse JSON log entry
	var logEntry map[string]interface{}
	if err := json.Unmarshal(p, &logEntry); err != nil {
		// Not JSON, skip
		return len(p), nil
	}

	// Check if this is an error log (only ERROR and FATAL, not WARN)
	level, ok := logEntry["level"].(string)
	if !ok || (level != "error" && level != "fatal") {
		return len(p), nil
	}

	// Extract fields
	message, _ := logEntry["message"].(string)
	errorMsg, _ := logEntry["error"].(string)
	timeStr, _ := logEntry["time"].(string)

	// Build notification message
	go n.sendNotification(message, errorMsg, timeStr, logEntry)

	return len(p), nil
}

func (n *AdminNotifier) WriteLevel(level string, p []byte) (int, error) {
	return n.Write(p)
}

func (n *AdminNotifier) sendNotification(message, errorMsg, timeStr string, logEntry map[string]interface{}) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Build a compact formatted message
	notificationMsg := "🚨 <b>Ошибка</b>\n"

	if message != "" {
		notificationMsg += html.EscapeString(message)
	}

	if errorMsg != "" {
		notificationMsg += "\n" + html.EscapeString(errorMsg)
	}

	// Add only important contextual fields
	var details []string
	if groupID, ok := logEntry["group_id"]; ok {
		details = append(details, fmt.Sprintf("группа: %v", groupID))
	}
	if userID, ok := logEntry["user_id"]; ok {
		details = append(details, fmt.Sprintf("юзер: %v", userID))
	}
	if pollID, ok := logEntry["poll_id"]; ok {
		details = append(details, fmt.Sprintf("опрос: %v", pollID))
	}

	if len(details) > 0 {
		notificationMsg += "\n<i>" + html.EscapeString(fmt.Sprintf("(%s)", joinStrings(details, ", "))) + "</i>"
	}

	for adminID := range n.adminIDs {
		opts := &echotron.MessageOptions{
			ParseMode: echotron.HTML,
		}
		if _, err := n.api.SendMessage(notificationMsg, adminID, opts); err != nil {
			// Fallback to stderr to avoid recursion with zerolog
			fmt.Fprintf(os.Stderr, "Failed to send admin notification to %d: %v\n", adminID, err)
		}
	}
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
