// Package logbuf keeps recent server logs in memory so administrators can read
// and live-tail them from the console without shell access to the container.
package logbuf

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Seq   int64          `json:"seq"`
	Time  time.Time      `json:"time"`
	Level string         `json:"level"`
	Msg   string         `json:"msg"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	next    int
	full    bool
	seq     int64
	subs    map[chan Entry]struct{}
}

func New(size int) *Buffer {
	return &Buffer{entries: make([]Entry, size), subs: map[chan Entry]struct{}{}}
}

func (b *Buffer) add(e Entry) {
	b.mu.Lock()
	b.seq++
	e.Seq = b.seq
	b.entries[b.next] = e
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
	for ch := range b.subs {
		select {
		case ch <- e:
		default: // slow subscriber: drop rather than block logging
		}
	}
	b.mu.Unlock()
}

// Snapshot returns entries in chronological order.
func (b *Buffer) Snapshot() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []Entry
	if b.full {
		out = append(out, b.entries[b.next:]...)
	}
	out = append(out, b.entries[:b.next]...)
	return append([]Entry(nil), out...)
}

func (b *Buffer) Capacity() int { return len(b.entries) }

func (b *Buffer) Subscribe() (chan Entry, func()) {
	ch := make(chan Entry, 256)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
	}
}

// Handler tees records into the buffer and into a JSON handler on w.
type Handler struct {
	buf    *Buffer
	next   slog.Handler
	attrs  []slog.Attr
	groups []string
	level  *slog.LevelVar
}

func NewHandler(buf *Buffer, w io.Writer, level *slog.LevelVar) *Handler {
	return &Handler{buf: buf, next: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}), level: level}
}

func (h *Handler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	attrs := map[string]any{}
	prefix := strings.Join(h.groups, ".")
	put := func(a slog.Attr) {
		k := a.Key
		if prefix != "" {
			k = prefix + "." + k
		}
		attrs[k] = a.Value.Resolve().Any()
	}
	for _, a := range h.attrs {
		put(a)
	}
	r.Attrs(func(a slog.Attr) bool { put(a); return true })
	for k, v := range attrs {
		if err, ok := v.(error); ok {
			attrs[k] = err.Error()
		}
	}
	h.buf.add(Entry{Time: r.Time, Level: r.Level.String(), Msg: r.Message, Attrs: attrs})
	return h.next.Handle(ctx, r)
}

func (h *Handler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr(nil), h.attrs...), as...)
	c.next = h.next.WithAttrs(as)
	return &c
}

func (h *Handler) WithGroup(name string) slog.Handler {
	c := *h
	c.groups = append(append([]string(nil), h.groups...), name)
	c.next = h.next.WithGroup(name)
	return &c
}
