package api

import "time"

const ProtocolVersion = 1

type ProcessMode string

const (
	ModeGuard       ProcessMode = "guard"
	ModeOnce        ProcessMode = "once"
	ModeCronRun     ProcessMode = "cronRun"
	ModeCronRestart ProcessMode = "cronRestart"
)

type ProcessDefinition struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Command          string            `json:"command"`
	Arguments        string            `json:"arguments,omitempty"`
	WorkingDirectory string            `json:"workingDirectory,omitempty"`
	Environment      map[string]string `json:"environment,omitempty"`
	Mode             ProcessMode       `json:"mode"`
	CronExpression   string            `json:"cronExpression,omitempty"`
	Enabled          bool              `json:"enabled"`
}

type ConfigSnapshot struct {
	SchemaVersion  int                 `json:"schemaVersion"`
	Revision       uint64              `json:"revision"`
	PathEnv        string              `json:"pathEnv"`
	GuardianPaused bool                `json:"guardianPaused"`
	Processes      []ProcessDefinition `json:"processes"`
}

type ProcessState string

const (
	StateDisabled  ProcessState = "disabled"
	StateStarting  ProcessState = "starting"
	StateRunning   ProcessState = "running"
	StateWaiting   ProcessState = "waiting"
	StateCompleted ProcessState = "completed"
	StateError     ProcessState = "error"
)

type ProcessRuntime struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	State     ProcessState `json:"state"`
	PID       int          `json:"pid,omitempty"`
	LastStart *time.Time   `json:"lastStart,omitempty"`
	LastExit  *time.Time   `json:"lastExit,omitempty"`
	ErrorCode string       `json:"errorCode,omitempty"`
	Error     string       `json:"error,omitempty"`
}

type RuntimeSnapshot struct {
	Mode      string           `json:"mode"`
	Paused    bool             `json:"paused"`
	Healthy   bool             `json:"healthy"`
	Error     string           `json:"error,omitempty"`
	Processes []ProcessRuntime `json:"processes"`
}

type LogEntry struct {
	Sequence  int64     `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
	Category  string    `json:"category"`
	Text      string    `json:"text"`
}

type LogBuffer struct {
	Capacity int        `json:"capacity"`
	Kept     int        `json:"kept"`
	Entries  []LogEntry `json:"entries"`
}

type LogSnapshot struct {
	ProcessID     string    `json:"processID"`
	TotalSeen     int64     `json:"totalSeen"`
	LineMaxBytes  int       `json:"lineMaxBytes"`
	ErrorWarning  LogBuffer `json:"errorWarning"`
	StandardOther LogBuffer `json:"standardOther"`
}

type Event struct {
	Sequence  uint64 `json:"sequence"`
	Type      string `json:"type"`
	ProcessID string `json:"processID,omitempty"`
	Revision  uint64 `json:"revision,omitempty"`
}
