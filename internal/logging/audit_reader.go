package logging

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func ReadAuditEvents(path string, limit int) ([]AuditEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open audit log file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	events := make([]AuditEvent, 0)
	for scanner.Scan() {
		var event AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("decode audit event: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan audit log file: %w", err)
	}

	if limit > 0 && len(events) > limit {
		return events[len(events)-limit:], nil
	}
	return events, nil
}
