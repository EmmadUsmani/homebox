package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/EmmadUsmani/homebox/internal/untrusted"
)

type NetDenial struct {
	Time        time.Time `json:"time"`
	Message     string    `json:"msg"`
	Protocol    string    `json:"protocol,omitempty"`
	Destination string    `json:"destination,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	Active      *uint     `json:"active,omitempty"`
	Limit       *uint     `json:"limit,omitempty"`
}

func readDenials(path string) ([]NetDenial, error) {
	const tailBytes = 128 << 10
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	offset := max(0, info.Size()-tailBytes)
	data, err := io.ReadAll(io.NewSectionReader(file, offset, tailBytes))
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		_, data, _ = bytes.Cut(data, []byte("\n"))
	}
	return parseDenials(data), nil
}

func parseDenials(data []byte) []NetDenial {
	const limit = 20
	var denials []NetDenial
	// The daemon writes newline-terminated records. A concurrent write may leave a partial final record.
	end := bytes.LastIndexByte(data, '\n')
	for end >= 0 && len(denials) < limit {
		start := bytes.LastIndexByte(data[:end], '\n') + 1
		line := data[start:end]
		var denial NetDenial
		if len(line) <= untrusted.MaxLine && json.Unmarshal(line, &denial) == nil &&
			(denial.Message == "egress denied" || denial.Message == "egress flow refused") && !denial.Time.IsZero() {
			denial.Protocol = untrusted.Line(denial.Protocol)
			denial.Destination = untrusted.Line(denial.Destination)
			denial.Reason = untrusted.Line(denial.Reason)
			denials = append(denials, denial)
		}
		end = start - 1
	}
	return denials
}
