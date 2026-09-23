package main

import (
	"bytes"
	"fmt"
	"io"
	"syscall/js"

	"github.com/bodgit/rvz"
)

type chunkWriter struct {
	cb   js.Value
	buf  []byte
	size int64
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		space := cap(w.buf) - len(w.buf)
		if space == 0 {
			w.flush()
			space = cap(w.buf)
		}
		n := len(p)
		if n > space {
			n = space
		}
		w.buf = append(w.buf, p[:n]...)
		p = p[n:]
		if len(w.buf) == cap(w.buf) {
			w.flush()
		}
	}
	return written, nil
}

func (w *chunkWriter) flush() {
	if len(w.buf) == 0 {
		return
	}
	out := js.Global().Get("Uint8Array").New(len(w.buf))
	js.CopyBytesToJS(out, w.buf)
	w.size += int64(len(w.buf))
	w.cb.Invoke(out, w.size)
	w.buf = w.buf[:0]
}

func convert(this js.Value, args []js.Value) any {
	if len(args) != 2 {
		return js.ValueOf(map[string]any{"error": "Expected RVZ bytes and a progress callback."})
	}
	src, cb := args[0], args[1]
	n := src.Get("byteLength").Int()
	input := make([]byte, n)
	js.CopyBytesToGo(input, src)

	r, err := rvz.NewReader(bytes.NewReader(input))
	if err != nil {
		return js.ValueOf(map[string]any{"error": fmt.Sprintf("RVZ error: %v", err)})
	}

	total := r.Size()
	w := &chunkWriter{cb: cb, buf: make([]byte, 0, 4*1024*1024)}
	buf := make([]byte, 4*1024*1024)
	var done int64

	for {
		n, er := r.Read(buf)
		if n > 0 {
			if _, ew := w.Write(buf[:n]); ew != nil {
				return js.ValueOf(map[string]any{"error": ew.Error()})
			}
			done += int64(n)
			cb.Invoke(js.Null(), done, total)
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return js.ValueOf(map[string]any{"error": er.Error()})
		}
	}
	w.flush()
	return js.ValueOf(map[string]any{"ok": true, "size": done})
}

func main() {
	js.Global().Set("rvzConvert", js.FuncOf(convert))
	select {}
}
