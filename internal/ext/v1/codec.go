package v1

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const DefaultMaxLineBytes = 1 << 20

var (
	ErrInvalidMessage = errors.New("invalid ext/v1 message")
	ErrLineTooLong    = errors.New("ext/v1 line too long")
)

// Decoder reads exactly one JSON object per line.
type Decoder struct {
	scanner *bufio.Scanner
}

// NewDecoder creates a decoder with the one-megabyte protocol line limit.
func NewDecoder(reader io.Reader) *Decoder {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), DefaultMaxLineBytes)
	return &Decoder{scanner: scanner}
}

// Decode reads the next envelope, rejecting blank lines, trailing JSON values,
// and unknown envelope fields.
func (decoder *Decoder) Decode() (Envelope, error) {
	if !decoder.scanner.Scan() {
		if err := decoder.scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				return Envelope{}, ErrLineTooLong
			}
			return Envelope{}, err
		}
		return Envelope{}, io.EOF
	}
	line := decoder.scanner.Bytes()
	if len(bytes.TrimSpace(line)) == 0 {
		return Envelope{}, fmt.Errorf("blank line: %w", ErrInvalidMessage)
	}
	var envelope Envelope
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&envelope); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w: %w", err, ErrInvalidMessage)
	}
	if err := ensureEOF(dec); err != nil {
		return Envelope{}, err
	}
	if envelope.Type == "" {
		return Envelope{}, fmt.Errorf("type is required: %w", ErrInvalidMessage)
	}
	return envelope, nil
}

// Encoder writes one compact JSON object and newline per message.
type Encoder struct{ encoder *json.Encoder }

func NewEncoder(writer io.Writer) *Encoder              { return &Encoder{encoder: json.NewEncoder(writer)} }
func (encoder *Encoder) Encode(envelope Envelope) error { return encoder.encoder.Encode(envelope) }

// Pack validates and wraps a typed message.
func Pack(id string, message Message) (Envelope, error) {
	if err := Validate(message); err != nil {
		return Envelope{}, err
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Type: message.MessageType(), ID: id, Payload: payload}, nil
}

// Unpack strictly decodes and validates an envelope payload.
func Unpack(envelope Envelope) (Message, error) {
	var message Message
	switch envelope.Type {
	case TypeHello:
		message = &Hello{}
	case TypeReady:
		message = &Ready{}
	case TypeSubscribe:
		message = &Subscribe{}
	case TypeEvent:
		message = &Event{}
	case TypeEmit:
		message = &EmitEvent{}
	case TypeDropped:
		message = &EventsDropped{}
	case TypeInboxAnswer:
		message = &InboxAnswer{}
	case TypeActionAdd:
		message = &RegisterActions{}
	case TypeActionInvoke:
		message = &InvokeAction{}
	case TypeError:
		message = &Error{}
	case TypeShutdown:
		message = &Shutdown{}
	default:
		return nil, &ProtocolError{Code: "unknown_type", Message: fmt.Sprintf("unknown message type %q", envelope.Type), ID: envelope.ID}
	}
	if len(envelope.Payload) == 0 {
		return nil, fmt.Errorf("%s payload is required: %w", envelope.Type, ErrInvalidMessage)
	}
	dec := json.NewDecoder(bytes.NewReader(envelope.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(message); err != nil {
		return nil, fmt.Errorf("decode %s payload: %w: %w", envelope.Type, err, ErrInvalidMessage)
	}
	if err := ensureEOF(dec); err != nil {
		return nil, err
	}
	if err := Validate(message); err != nil {
		return nil, err
	}
	return message, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON value: %w", ErrInvalidMessage)
		}
		return fmt.Errorf("trailing data: %w: %w", err, ErrInvalidMessage)
	}
	return nil
}
