package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// Audit logging
func auditEvent(eventType string, details interface{}) error {
	logEntry := map[string]interface{}{
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"event_type": eventType,
		"details":    details,
	}

	logJSON, err := json.Marshal(logEntry)
	if err != nil {
		return fmt.Errorf("marshaling audit log entry: %w", err)
	}

	// Append to JSON log file following FHS standards
	file, err := os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: failed to open audit log file: %v", err)
		return nil
	}
	defer file.Close()

	if _, err := file.WriteString(string(logJSON) + "\n"); err != nil {
		log.Printf("Warning: failed to write audit log: %v", err)
	}

	return nil
}

func ParseAuditEntry(line string) (map[string]interface{}, error) {
	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil, fmt.Errorf("unmarshaling audit entry: %w", err)
	}
	return entry, nil
}

// Helper functions for JSON parsing
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return 0
}
