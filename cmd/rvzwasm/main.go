package main

import (
	"fmt"
	"io"
	"syscall/js"

	"github.com/bodgit/rvz"
)

const inputCacheSize = 2 * 1024 * 1024

type jsReaderAt struct {
	readFn      js.Value
	size        int64
	cache       []byte
	cacheStart  int64
	cacheLength int
}

func (r *jsReaderAt) fill(off int64) error {
	if off < 0 || off >= r.size {
		return io.EOF
	}

	start := off - (off % inputCacheSize)
	want := int64(inputCacheSize)
	if remaining := r.size - start; remaining < want {
		want = remaining
	}

	out := r.readFn.Invoke(start, want)
	n := js.CopyBytesToGo(r.cache[:int(want)], out)
	if n <= 0 {
		return io.EOF
	}

	r.cacheStart = start
	r.cacheLength = n
	return nil
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

		if current < r.cacheStart || current >= r.cacheStart+int64(r.cacheLength) {
			if err := r.fill(current); err != nil {
				break
			}
		}

		inside := current - r.cacheStart
		available := r.cacheLength - int(inside)
		if available <= 0 {
			break
		}

		want := len(p) - total
		if want > available {
			want = available
		}
		if remaining := int(r.size - current); want > remaining {
			want = remaining
		}

		copy(p[total:total+want], r.cache[int(inside):int(inside)+want])
		total += want
	}

	if total == len(p) {
		return total, nil
	}
	return total, io.EOF
}

var (
	converter rvz.Reader
	writeFn   js.Value
	outBuffer []byte
	jsBuffer  js.Value
	outOffset int64
	totalSize int64
	doneSize  int64
)

func start(this js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = js.ValueOf(map[string]any{"error": fmt.Sprintf("WASM startup panic: %v", r)})
		}
	}()

	readFn := js.Global().Get("rvzInputRead")
	writeFn = js.Global().Get("rvzOutputWrite")
	sizeFn := js.Global().Get("rvzInputSize")
	if !readFn.Truthy() || !writeFn.Truthy() || !sizeFn.Truthy() {
		return js.ValueOf(map[string]any{"error": "Local file bridge is not ready."})
	}

	size := int64(sizeFn.Invoke().Float())
	readerAt := &jsReaderAt{
		readFn: readFn,
		size:   size,
		cache:  make([]byte, inputCacheSize),
	}

	r, err := rvz.NewReader(readerAt)
	if err != nil {
		return js.ValueOf(map[string]any{"error": fmt.Sprintf("RVZ error: %v", err)})
	}

	const batchSize = 256 * 1024
	converter = r
	outBuffer = make([]byte, batchSize)
	jsBuffer = js.Global().Get("Uint8Array").New(batchSize)
	outOffset = 0
	totalSize = r.Size()
	doneSize = 0

	return js.ValueOf(map[string]any{"ok": true, "size": totalSize})
}

func step(this js.Value, args []js.Value) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = js.ValueOf(map[string]any{"error": fmt.Sprintf("WASM decoder panic: %v", r)})
		}
	}()

	if converter == nil {
		return js.ValueOf(map[string]any{"error": "Converter is not started."})
	}

	n, er := converter.Read(outBuffer)
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

	if er == io.EOF {
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
