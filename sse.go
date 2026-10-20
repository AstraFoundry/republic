package republic

import (
	"bufio"
	"bytes"
	"io"
	"strings"
)

type ServerSentEvent struct {
	Event string
	Data  string
}

// ReadSSE reads arbitrarily long events without Scanner's default 64 KiB limit.
// Returning an error from yield stops reading immediately.
func ReadSSE(r io.Reader, endMarker string, yield func(ServerSentEvent) error) error {
	reader := bufio.NewReader(r)
	event := ""
	data := []string{}
	skipLF := false
	flush := func() (bool, error) {
		if len(data) == 0 {
			event = ""
			return false, nil
		}
		payload := strings.Join(data, "\n")
		name := event
		if name == "" {
			name = "message"
		}
		event = ""
		data = nil
		err := yield(ServerSentEvent{name, payload})
		return endMarker != "" && payload == endMarker, err
	}
	for {
		line, ended, err := readSSELine(reader, &skipLF)
		if len(line) > 0 || ended {
			if line == "" {
				stop, e := flush()
				if e != nil {
					return e
				}
				if stop {
					return nil
				}
			} else {
				field, value, _ := strings.Cut(line, ":")
				value = strings.TrimPrefix(value, " ")
				switch field {
				case "event":
					event = value
				case "data":
					data = append(data, value)
				}
			}
		}
		if err != nil {
			if err != io.EOF {
				return err
			}
			_, e := flush()
			return e
		}
	}
}

func readSSELine(r *bufio.Reader, skipLF *bool) (string, bool, error) {
	var b bytes.Buffer
	for {
		c, err := r.ReadByte()
		if err != nil {
			return b.String(), false, err
		}
		if *skipLF {
			*skipLF = false
			if c == '\n' {
				continue
			}
		}
		switch c {
		case '\n':
			return b.String(), true, nil
		case '\r':
			// Delay consuming the optional LF until the next call so that a
			// CR-terminated event does not wait for another network packet.
			*skipLF = true
			return b.String(), true, nil
		default:
			b.WriteByte(c)
		}
	}
}
