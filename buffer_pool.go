package main

import (
	"bytes"
	"runtime/pprof"
	"sync"
)

func GetPooledBuffer() *PooledBuffer {
	b := bufferPool.Get().(*PooledBuffer)
	bufferPoolProfile.Add(b, 1)
	return b
}

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(PooledBuffer)
	},
}

type PooledBuffer struct {
	bytes.Buffer
}

func (b *PooledBuffer) Close() error {
	b.Reset()
	bufferPoolProfile.Remove(b)
	bufferPool.Put(b)
	return nil
}

var bufferPoolProfile = pprof.NewProfile("buffer.pool")
