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

func convert(this js.Value, args []js.Value) any {
	readFn := js.Global().Get("rvzInputRead")
	writeFn := js.Global().Get("rvzOutputWrite")
	sizeFn := js.Global().Get("rvzInputSize")
	if !readFn.Truthy() || !writeFn.Truthy() || !sizeFn.Truthy() {
		return js.ValueOf(map[string]any{"error": "Local file bridge is not ready."})
	}

	size := int64(sizeFn.Invoke().Float())
	r, err := rvz.NewReader(&jsReaderAt{readFn: readFn, size: size})
	if err != nil {
		return js.ValueOf(map[string]any{"error": fmt.Sprintf("RVZ error: %v", err)})
	}

	// 50 MiB batch: larger than the previous mobile-safe setting,
	// while avoiding the 100 MiB + 100 MiB memory pressure that killed the tab.
	const batchSize = 50 * 1024 * 1024
	out := make([]byte, batchSize)
	total := r.Size()
	var done int64
	var outOffset int64
	lastProgress := int64(0)

	jsBuf := js.Global().Get("Uint8Array").New(batchSize)

	flush := func(n int) error {
		if n == 0 {
			return nil
		}
		js.CopyBytesToJS(jsBuf, out[:n])
		data := jsBuf
		if n != batchSize {
			data = jsBuf.Call("subarray", 0, n)
		}
		written := writeFn.Invoke(data, outOffset).Int()
		if written != n {
			return io.ErrShortWrite
		}
		outOffset += int64(n)
		return nil
	}

	pending := 0
	for {
		n, er := r.Read(out[pending:])
		pending += n

		if pending == batchSize || er == io.EOF {
			if err := flush(pending); err != nil {
				return js.ValueOf(map[string]any{"error": err.Error()})
			}
			done += int64(pending)
			pending = 0

			if done-lastProgress >= 16*1024*1024 || er == io.EOF {
				js.Global().Get("rvzProgress").Invoke(done, total)
				lastProgress = done
			}
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
