// Package protocol defines the panel ↔ helper WebSocket messages (v0).
//
// 모든 메시지는 텍스트 프레임 하나에 JSON 하나: {"v":0,"type":"...","id":"...","data":{...}}.
// id는 요청에 붙이면 응답에 그대로 돌려준다. 계약의 기준은 /protocol/fixtures/*.json 이며
// Go(이 패키지 테스트)와 TS(panel/core 테스트)가 같은 픽스처로 양쪽 타입을 검증한다.
package protocol

import (
	"encoding/json"
	"fmt"

	"birdcage/helper/internal/engine"
	"birdcage/helper/internal/jobs"
	"birdcage/helper/internal/tools"
)

// Version is the protocol version carried in every envelope.
const Version = 0

// Message types.
const (
	// panel → helper
	TypeHello       = "hello"
	TypeJobCreate   = "job.create"
	TypeJobCancel   = "job.cancel"
	TypeJobList     = "job.list"
	TypeToolsEnsure = "tools.ensure"
	TypeMediaProbe  = "media.probe"

	// helper → panel
	TypeHelloOK     = "hello.ok"
	TypeJobAccepted = "job.accepted"
	TypeJobUpdate   = "job.update"
	TypeJobDone     = "job.done"
	TypeJobFailed   = "job.failed"
	TypeJobListOK   = "job.list.ok"
	TypeToolsStatus = "tools.status"
	TypeMediaInfo   = "media.info"
	TypeError       = "error"
)

// Envelope wraps every message.
type Envelope struct {
	V    int             `json:"v"`
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Host describes the Premiere instance the panel runs in.
type Host struct {
	App     string `json:"app"`     // "PPRO"
	Version string `json:"version"` // e.g. "15.2.0"
	API     string `json:"api"`     // "cep" | "uxp"
	OS      string `json:"os"`      // "mac" | "win"
}

// Hello is the first message from a panel.
type Hello struct {
	Client        string `json:"client"`
	ClientVersion string `json:"clientVersion"`
	Host          Host   `json:"host"`
}

// HelloOK answers Hello.
type HelloOK struct {
	HelperVersion string          `json:"helperVersion"`
	Protocol      int             `json:"protocol"`
	Profile       string          `json:"profile"` // normalization profile chosen for this host
	LibraryDir    string          `json:"libraryDir"`
	Tools         []tools.Status  `json:"tools"`
	Jobs          []jobs.Snapshot `json:"jobs"`
}

// JobCreate asks for a download.
type JobCreate struct {
	URL       string `json:"url"`
	MaxHeight int    `json:"maxHeight,omitempty"` // 0 → helper default (1080)
	ProbeID   string `json:"probeId,omitempty"`   // media.info probeId: download without re-extraction
	FormatID  string `json:"formatId,omitempty"`  // chosen variant (media.info variants[].formatId)
	// Range downloads only this part of the source (plus 1s handles); job.done carries clip In/Out.
	Range *engine.Section `json:"range,omitempty"`
}

// MediaProbe asks for a link's metadata before downloading (answered by media.info, same id).
type MediaProbe struct {
	URL       string `json:"url"`
	MaxHeight int    `json:"maxHeight,omitempty"`
}

// MediaInfo answers MediaProbe with either the info or the classified failure.
type MediaInfo struct {
	URL     string          `json:"url"`
	ProbeID string          `json:"probeId,omitempty"`
	Info    *engine.Info    `json:"info,omitempty"`
	Failure *engine.Failure `json:"failure,omitempty"`
}

// JobAccepted answers JobCreate.
type JobAccepted struct {
	JobID string `json:"jobId"`
}

// JobCancel stops a job.
type JobCancel struct {
	JobID string `json:"jobId"`
}

// JobEvent carries a job snapshot (job.update / job.done / job.failed).
type JobEvent struct {
	Job jobs.Snapshot `json:"job"`
}

// JobList answers job.list.
type JobList struct {
	Jobs []jobs.Snapshot `json:"jobs"`
}

// ToolsEnsure asks the helper to install missing tools.
type ToolsEnsure struct {
	Channel string `json:"channel,omitempty"` // yt-dlp channel: "stable" (default) | "nightly"
}

// ToolsStatus reports tool availability (also sent while installing).
type ToolsStatus struct {
	Installing bool           `json:"installing"`
	Tools      []tools.Status `json:"tools"`
	Error      string         `json:"error,omitempty"`
}

// Error reports a protocol-level problem with a request.
type Error struct {
	Code    string `json:"code"` // bad_request | unknown_type | internal
	Message string `json:"message"`
}

// Encode builds an envelope.
func Encode(typ, id string, data any) ([]byte, error) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	return json.Marshal(Envelope{V: Version, Type: typ, ID: id, Data: raw})
}

// Decode parses an envelope and checks the version.
func Decode(b []byte) (Envelope, error) {
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return e, err
	}
	if e.V != Version {
		return e, fmt.Errorf("unsupported protocol version %d", e.V)
	}
	if e.Type == "" {
		return e, fmt.Errorf("missing type")
	}
	return e, nil
}

// DecodeData unmarshals the payload into v.
func DecodeData(e Envelope, v any) error {
	if len(e.Data) == 0 {
		return fmt.Errorf("%s: missing data", e.Type)
	}
	return json.Unmarshal(e.Data, v)
}
