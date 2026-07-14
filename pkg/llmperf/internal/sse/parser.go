package sse

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMalformed     = errors.New("malformed SSE stream")
	ErrMetadataLimit = errors.New("SSE metadata exceeds limit")
	ErrDataLimit     = errors.New("SSE event data exceeds limit")
)

// Event is one dispatched SSE data event.
type Event struct {
	Sequence    uint64
	Type        string
	ID          string
	RetryMillis *int64
	Data        []byte
	At          time.Time
}

// Parser incrementally implements SSE framing. It is not concurrency-safe.
type Parser struct {
	maxMetadataBytes int
	maxDataBytes     int
	onEvent          func(Event) error

	field         []byte
	value         []byte
	data          []byte
	state         lineState
	skipLF        bool
	dataLines     int
	metadataBytes int
	eventType     string
	retryMillis   *int64
	lastID        string
	finished      bool
	offset        int64
	sequence      uint64
	bom           []byte
	bomDone       bool
	currentAt     time.Time
}

type lineState uint8

const (
	lineField lineState = iota
	lineSkipSpace
	lineValue
	lineData
	lineIgnore
)

func NewParser(maxMetadataBytes, maxDataBytes int, onEvent func(Event) error) *Parser {
	return &Parser{maxMetadataBytes: maxMetadataBytes, maxDataBytes: maxDataBytes, onEvent: onEvent}
}

func (p *Parser) Offset() int64    { return p.offset }
func (p *Parser) Sequence() uint64 { return p.sequence }

func (p *Parser) FeedAt(at time.Time, data []byte) error {
	if p.finished {
		return ErrMalformed
	}
	p.currentAt = at
	for _, b := range data {
		p.offset++
		if !p.bomDone {
			ready, buffered := p.consumeBOM(b)
			if !ready {
				continue
			}
			for _, bufferedByte := range buffered {
				if err := p.writeByte(bufferedByte); err != nil {
					return err
				}
			}
			continue
		}
		if err := p.writeByte(b); err != nil {
			return err
		}
	}
	return nil
}

func (p *Parser) FinishAt(at time.Time) error {
	if p.finished {
		return nil
	}
	p.finished = true
	p.currentAt = at
	if !p.bomDone && len(p.bom) > 0 {
		for _, b := range p.bom {
			if err := p.writeByte(b); err != nil {
				return err
			}
		}
	}
	if len(p.field) > 0 || len(p.value) > 0 || p.state != lineField {
		if err := p.endLine(); err != nil {
			return err
		}
	}
	return p.dispatch()
}

func (p *Parser) consumeBOM(b byte) (bool, []byte) {
	p.bom = append(p.bom, b)
	want := []byte{0xef, 0xbb, 0xbf}
	for index := range p.bom {
		if p.bom[index] != want[index] {
			p.bomDone = true
			buffered := append([]byte(nil), p.bom...)
			p.bom = nil
			return true, buffered
		}
	}
	if len(p.bom) == len(want) {
		p.bomDone = true
		p.bom = nil
		return true, nil
	}
	return false, nil
}

func (p *Parser) writeByte(b byte) error {
	if p.skipLF {
		p.skipLF = false
		if b == '\n' {
			return nil
		}
	}
	if b == '\r' {
		if err := p.endLine(); err != nil {
			return err
		}
		p.skipLF = true
		return nil
	}
	if b == '\n' {
		return p.endLine()
	}
	switch p.state {
	case lineField:
		if b == ':' {
			return p.startValue()
		}
		return p.appendMetadata(&p.field, b)
	case lineSkipSpace:
		if err := p.beginValue(); err != nil {
			return err
		}
		if b == ' ' {
			return nil
		}
		return p.writeValueByte(b)
	case lineValue:
		return p.appendMetadata(&p.value, b)
	case lineData:
		return p.appendData(b)
	case lineIgnore:
		return nil
	default:
		return ErrMalformed
	}
}

func (p *Parser) writeValueByte(b byte) error {
	if p.state == lineData {
		return p.appendData(b)
	}
	if p.state == lineValue {
		return p.appendMetadata(&p.value, b)
	}
	return nil
}

func (p *Parser) startValue() error {
	key := string(p.field)
	p.field = p.field[:0]
	switch key {
	case "event", "id", "retry", "data":
		p.field = append(p.field, key...)
		p.state = lineSkipSpace
	default:
		p.state = lineIgnore
	}
	return nil
}

func (p *Parser) beginValue() error {
	if string(p.field) == "data" {
		if p.dataLines > 0 {
			if err := p.appendData('\n'); err != nil {
				return err
			}
		}
		p.dataLines++
		p.state = lineData
		return nil
	}
	p.state = lineValue
	return nil
}

func (p *Parser) endLine() error {
	switch p.state {
	case lineField:
		if len(p.field) == 0 {
			if err := p.dispatch(); err != nil {
				return err
			}
		} else {
			switch string(p.field) {
			case "data":
				if err := p.beginValue(); err != nil {
					return err
				}
			case "event":
				p.eventType = ""
			case "id":
				p.lastID = ""
			}
		}
	case lineValue:
		switch string(p.field) {
		case "event":
			p.eventType = string(p.value)
		case "id":
			if !strings.ContainsRune(string(p.value), '\x00') {
				p.lastID = string(p.value)
			}
		case "retry":
			value, err := strconv.ParseInt(string(p.value), 10, 64)
			if err == nil && value >= 0 {
				p.retryMillis = &value
			}
		}
	case lineSkipSpace:
		key := string(p.field)
		if err := p.beginValue(); err != nil {
			return err
		}
		switch key {
		case "event":
			p.eventType = ""
		case "id":
			p.lastID = ""
		}
	}
	p.field = p.field[:0]
	p.value = p.value[:0]
	p.state = lineField
	return nil
}

func (p *Parser) dispatch() error {
	if p.dataLines == 0 {
		p.resetEvent()
		return nil
	}
	eventType := p.eventType
	if eventType == "" {
		eventType = "message"
	}
	p.sequence++
	event := Event{
		Sequence:    p.sequence,
		Type:        eventType,
		ID:          p.lastID,
		RetryMillis: p.retryMillis,
		Data:        append([]byte(nil), p.data...),
		At:          p.currentAt,
	}
	if p.onEvent != nil {
		if err := p.onEvent(event); err != nil {
			return err
		}
	}
	p.resetEvent()
	return nil
}

func (p *Parser) resetEvent() {
	p.eventType = ""
	p.retryMillis = nil
	p.data = p.data[:0]
	p.dataLines = 0
	p.metadataBytes = 0
}

func (p *Parser) appendMetadata(dst *[]byte, b byte) error {
	if p.maxMetadataBytes > 0 && p.metadataBytes+1 > p.maxMetadataBytes {
		return ErrMetadataLimit
	}
	*dst = append(*dst, b)
	p.metadataBytes++
	return nil
}

func (p *Parser) appendData(b byte) error {
	if p.maxDataBytes > 0 && len(p.data)+1 > p.maxDataBytes {
		return ErrDataLimit
	}
	p.data = append(p.data, b)
	return nil
}
