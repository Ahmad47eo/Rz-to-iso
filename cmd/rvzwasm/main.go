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
	want := len(p)
	if int64(want) > r.size-off {
		want = int(r.size - off)
	}
	if want == 0 {
		return 0, io.EOF
	}
	out := r.readFn.Invoke(off, want)
	n := js.CopyBytesToGo(p[:want], out)
	if n != want {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

type jsWriter struct {
	writeFn js.Value
	size    int64
	buf     js.Value
	capacity int
}

func (w *jsWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(p) > w.capacity {
		w.capacity = len(p)
		w.buf = js.Global().Get("Uint8Array").New(w.capacity)
	}
	js.CopyBytesToJS(w.buf, p)
	n := w.writeFn.Invoke(w.buf, w.size).Int()
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	w.size += int64(n)
	return n, nil
}

func convert(this js.Value,args []js.Value) any {
	readFn:=js.Global().Get("rvzInputRead")
	writeFn:=js.Global().Get("rvzOutputWrite")
	sizeFn:=js.Global().Get("rvzInputSize")
	if !readFn.Truthy()||!writeFn.Truthy()||!sizeFn.Truthy(){
		return js.ValueOf(map[string]any{"error":"Local file bridge is not ready."})
	}
	size:=int64(sizeFn.Invoke().Float())
	r,err:=rvz.NewReader(&jsReaderAt{readFn:readFn,size:size})
	if err!=nil{return js.ValueOf(map[string]any{"error":fmt.Sprintf("RVZ error: %v",err)})}

	// Keep decompressed data in a large reusable buffer and cross the JS/WASM
	// boundary only once per 16 MiB instead of once per Read().
	const batchSize=16*1024*1024
	out:=make([]byte,batchSize)
	total:=r.Size()
	var done int64
	var outOffset int64
	lastProgress:=int64(0)

	flush:=func(n int) error {
		if n==0{return nil}
		buf:=js.Global().Get("Uint8Array").New(n)
		js.CopyBytesToJS(buf,out[:n])
		written:=writeFn.Invoke(buf,outOffset).Int()
		if written!=n{return io.ErrShortWrite}
		outOffset+=int64(n)
		return nil
	}

	for {
		n,er:=r.Read(out)
		if n>0 {
			if err:=flush(n);err!=nil{
				return js.ValueOf(map[string]any{"error":err.Error()})
			}
			done+=int64(n)
			if done-lastProgress >= 64*1024*1024 || er==io.EOF {
				js.Global().Get("rvzProgress").Invoke(done,total)
				lastProgress=done
			}
		}
		if er==io.EOF{break}
		if er!=nil{return js.ValueOf(map[string]any{"error":er.Error()})}
	}
	return js.ValueOf(map[string]any{"ok":true,"size":done})
}
func main() {
	js.Global().Set("rvzConvert", js.FuncOf(convert))
	select {}
}
