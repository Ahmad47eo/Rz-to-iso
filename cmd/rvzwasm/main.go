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
	for len(p) > 0 {
		n := copy(w.buf, p)
		p = p[n:]
		w.size += int64(n)
		if len(w.buf) == cap(w.buf) {
			w.flush()
		} else if n == 0 {
			w.flush()
		}
	}
	return len(p), nil
}

func (w *chunkWriter) flush() {
	if len(w.buf) == 0 {
		return
	}
	out := js.Global().Get("Uint8Array").New(len(w.buf))
	js.CopyBytesToJS(out, w.buf)
	w.cb.Invoke(out, w.size)
	w.buf = w.buf[:0]
}

func convert(this js.Value, args []js.Value) any {
	if len(args) != 2 {
		return js.ValueOf(map[string]any{"error": "Expected RVZ bytes and a progress callback."})
	}

	src := args[0]
	cb := args[1]

	if src.Type() != js.TypeObject {
		return js.ValueOf(map[string]any{"error": "Invalid input."})
	}

	n := src.Get("byteLength").Int()
	input := make([]byte, n)
	js.CopyBytesToGo(input, src)

	reader := bytes.NewReader(input)
	r, err := rvz.NewReader(reader)
	if err != nil {
		return js.ValueOf(map[string]any{"error": fmt.Sprintf("RVZ error: %v", err)})
	}

	total := r.Size()
	w := &chunkWriter{cb: cb, buf: make([]byte, 0, 4*1024*1024)}

	// Copy in reasonably large chunks so the browser stays responsive.
	buf := make([]byte, 4*1024*1024)
	var done int64
	for {
		n, er := r.Read(buf)
		if n > 0 {
			if _, ew := w.Write(buf[:n]); ew != nil {
				return js.ValueOf(map[string]any{"error": ew.Error()})
			}
			done += int64(n)
			cb.Invoke(js.ValueOf(nil), done, total)
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
