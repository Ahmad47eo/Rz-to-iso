package main

import (
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

	total := 0
	for total < len(p) {
		current := off + int64(total)
		if current >= r.size {
			break
		}

		want := len(p) - total
		remaining := r.size - current
		if int64(want) > remaining {
			want = int(remaining)
		}

		out := r.readFn.Invoke(current, want)
		n := js.CopyBytesToGo(p[total:total+want], out)
		if n <= 0 {
			break
		}
		total += n
	}

	if total == len(p) {
		return total, nil
	}
	return total, io.EOF
}

var (
	converter     *rvz.Reader
	writeFn       js.Value
	outBuffer     []byte
	jsBuffer      js.Value
	outOffset     int64
	totalSize     int64
	doneSize      int64
)

func start(this js.Value, args []js.Value) any {
	readFn := js.Global().Get("rvzInputRead")
	writeFn = js.Global().Get("rvzOutputWrite")
	sizeFn := js.Global().Get("rvzInputSize")
	if !readFn.Truthy() || !writeFn.Truthy() || !sizeFn.Truthy() {
		return js.ValueOf(map[string]any{"error": "Local file bridge is not ready."})
	}

	size := int64(sizeFn.Invoke().Float())
	r, err := rvz.NewReader(&jsReaderAt{readFn: readFn, size: size})
	if err != nil {
		return js.ValueOf(map[string]any{"error": fmt.Sprintf("RVZ error: %v", err)})
	}

	const batchSize = 1 * 1024 * 1024
	converter = r
	outBuffer = make([]byte, batchSize)
	jsBuffer = js.Global().Get("Uint8Array").New(batchSize)
	outOffset = 0
	totalSize = r.Size()
	doneSize = 0

	return js.ValueOf(map[string]any{"ok": true, "size": totalSize})
}

func step(this js.Value, args []js.Value) any {
	if converter == nil {
		return js.ValueOf(map[string]any{"error": "Converter is not started."})
	}

	n, er := io.ReadFull(converter, outBuffer)
	if n > 0 {
		js.CopyBytesToJS(jsBuffer, outBuffer[:n])
		data := jsBuffer
		if n != len(outBuffer) {
			data = jsBuffer.Call("subarray", 0, n)
		}

		written := writeFn.Invoke(data, outOffset).Int()
		if written != n {
			return js.ValueOf(map[string]any{"error": io.ErrShortWrite.Error()})
		}

		outOffset += int64(n)
		doneSize += int64(n)
	}

	if er == io.EOF || er == io.ErrUnexpectedEOF {
		result := map[string]any{"done": true, "size": doneSize}
		converter = nil
		outBuffer = nil
		jsBuffer = js.Undefined()
		return js.ValueOf(result)
	}

	if er != nil {
		return js.ValueOf(map[string]any{"error": er.Error()})
	}

	return js.ValueOf(map[string]any{"done": false, "size": doneSize, "total": totalSize})
}

func main() {
	js.Global().Set("rvzStart", js.FuncOf(start))
	js.Global().Set("rvzStep", js.FuncOf(step))
	select {}
}
