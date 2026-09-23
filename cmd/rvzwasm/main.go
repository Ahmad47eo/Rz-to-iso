package main

import (
	"errors"
	"fmt"
	"io"
	"syscall/js"

	"github.com/bodgit/rvz"
)

type jsReaderAt struct {
	readFn js.Value
	size   int64
}

func (r *jsReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= r.size {
		return 0, io.EOF
	}
	want := len(p)
	if int64(want) > r.size-off {
		want = int(r.size - off)
	}
	if want == 0 {
		return 0, io.EOF
	}
	out := r.readFn.Invoke(off, want)
	if out.Type() != js.TypeObject {
		return 0, errors.New("rvz: input read failed")
	}
	n := js.CopyBytesToGo(p[:want], out)
	if n != want {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

type jsWriter struct {
	writeFn js.Value
	size    int64
}

func (w *jsWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buf := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(buf, p)
	result := w.writeFn.Invoke(buf, w.size)
	n := result.Int()
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	w.size += int64(n)
	return n, nil
}

func convert(this js.Value, args []js.Value) any {
	if len(args) != 0 {
		return js.ValueOf(map[string]any{"error": "Expected no arguments."})
	}

	readFn := js.Global().Get("rvzInputRead")
	writeFn := js.Global().Get("rvzOutputWrite")
	sizeFn := js.Global().Get("rvzInputSize")
	if !readFn.Truthy() || !writeFn.Truthy() || !sizeFn.Truthy() {
		return js.ValueOf(map[string]any{"error": "Local file bridge is not ready."})
	}

	size := int64(sizeFn.Invoke().Int())
	r, err := rvz.NewReader(&jsReaderAt{readFn: readFn, size: size})
	if err != nil {
		return js.ValueOf(map[string]any{"error": fmt.Sprintf("RVZ error: %v", err)})
	}

	w := &jsWriter{writeFn: writeFn}
	buf := make([]byte, 4*1024*1024)
	var done int64
	total := r.Size()

	for {
		n, er := r.Read(buf)
		if n > 0 {
			if _, ew := w.Write(buf[:n]); ew != nil {
				return js.ValueOf(map[string]any{"error": ew.Error()})
			}
			done += int64(n)
			js.Global().Get("rvzProgress").Invoke(done, total)
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return js.ValueOf(map[string]any{"error": er.Error()})
		}
	}
	return js.ValueOf(map[string]any{"ok": true, "size": done})
}

func main() {
	js.Global().Set("rvzConvert", js.FuncOf(convert))
	select {}
}
