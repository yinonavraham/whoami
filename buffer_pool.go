package main

import (
	"bytes"
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

var bufferPoolProfile = dummyProfile{}

//var bufferPoolProfile = pprof.NewProfile("buffer.pool")

type dummyProfile struct{}

func (p dummyProfile) Add(v interface{}, skip int) {}
func (p dummyProfile) Remove(v interface{})        {}
