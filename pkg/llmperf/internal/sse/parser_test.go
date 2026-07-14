package sse

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParserHandlesGrammarAcrossEveryBoundary(t *testing.T) {
	input := []byte("\xef\xbb\xbf: hello\r\nid: 7\revent: update\ndata: first\r\ndata: second\r\nretry: 1500\r\n\r\ndata\n\n")
	wantData := []string{"first\nsecond", ""}
	started := time.Unix(100, 0)

	for split := 0; split <= len(input); split++ {
		var events []Event
		parser := NewParser(1024, 1024, func(event Event) error {
			events = append(events, event)
			return nil
		})
		if err := parser.FeedAt(started, input[:split]); err != nil {
			t.Fatalf("split %d: %v", split, err)
		}
		if err := parser.FeedAt(started.Add(time.Second), input[split:]); err != nil {
			t.Fatalf("split %d: %v", split, err)
		}
		if err := parser.FinishAt(started.Add(2 * time.Second)); err != nil {
			t.Fatalf("split %d finish: %v", split, err)
		}
		gotData := make([]string, len(events))
		for index := range events {
			gotData[index] = string(events[index].Data)
		}
		if !reflect.DeepEqual(gotData, wantData) {
			t.Fatalf("split %d data: %#v", split, gotData)
		}
		if len(events) != 2 || events[0].Sequence != 1 || events[0].Type != "update" || events[0].ID != "7" || events[0].RetryMillis == nil || *events[0].RetryMillis != 1500 || events[1].Type != "message" {
			t.Fatalf("split %d events: %#v", split, events)
		}
	}
}

func TestParserUsesDispatchingChunkTimestamp(t *testing.T) {
	first := time.Unix(100, 0)
	second := first.Add(time.Second)
	var events []Event
	parser := NewParser(1024, 1024, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err := parser.FeedAt(first, []byte("data: one\n")); err != nil {
		t.Fatal(err)
	}
	if err := parser.FeedAt(second, []byte("\ndata: two\n\n")); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || !events[0].At.Equal(second) || !events[1].At.Equal(second) {
		t.Fatalf("unexpected timestamps: %#v", events)
	}
}

func TestParserBoundsMetadataAndData(t *testing.T) {
	parser := NewParser(9, 1024, nil)
	if err := parser.FeedAt(time.Now(), []byte("event: abcde\n")); !errors.Is(err, ErrMetadataLimit) {
		t.Fatalf("expected metadata limit, got %v", err)
	}
	parser = NewParser(1024, 4, nil)
	if err := parser.FeedAt(time.Now(), []byte("data: abcde\n\n")); !errors.Is(err, ErrDataLimit) {
		t.Fatalf("expected data limit, got %v", err)
	}
	parser = NewParser(4, 1<<20+1, nil)
	if err := parser.FeedAt(time.Now(), []byte("data: "+strings.Repeat("x", 1<<20)+"\n\n")); err != nil {
		t.Fatalf("data must not consume metadata budget: %v", err)
	}
}

func TestParserDoesNotRetainInputBuffer(t *testing.T) {
	var got string
	parser := NewParser(1024, 1024, func(event Event) error {
		got = string(event.Data)
		return nil
	})
	buffer := []byte("data: value\n\n")
	if err := parser.FeedAt(time.Now(), buffer); err != nil {
		t.Fatal(err)
	}
	for index := range buffer {
		buffer[index] = 'x'
	}
	if got != "value" {
		t.Fatalf("retained caller buffer: %q", got)
	}
}

func FuzzParser(f *testing.F) {
	f.Add([]byte("event: response.completed\ndata: {}\n\n"))
	f.Add([]byte("\xef\xbb\xbfdata: one\r\ndata: two\r\r"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<16 {
			t.Skip()
		}
		parser := NewParser(4096, 4096, func(Event) error { return nil })
		_ = parser.FeedAt(time.Unix(100, 0), data)
		_ = parser.FinishAt(time.Unix(101, 0))
	})
}
