package main

import (
	"encoding/binary"
	"errors"
	"sync"
)

const (
	headerEntrySize = 4
	defaultValue    = 1024 // For this example we use 1024 like default value.
)

type cacheShard struct {
	items        map[uint64]uint32
	lock         sync.RWMutex
	array        []byte
	tail         int
	headerBuffer []byte
}

func initNewShard() *cacheShard {
	return &cacheShard{
		items:        make(map[uint64]uint32, defaultValue),
		array:        make([]byte, defaultValue),
		tail:         1,
		headerBuffer: make([]byte, headerEntrySize),
	}
}

func (s *cacheShard) set(hashedKey uint64, entry []byte) {
	w := wrapEntry(entry)
	s.lock.Lock()
	index := s.push(w)
	s.items[hashedKey] = uint32(index)
	s.lock.Unlock()
}

func (s *cacheShard) push(data []byte) int {
	dataLen := len(data)
	index := s.tail
	s.save(data, dataLen)
	return index
}

func (s *cacheShard) save(data []byte, len int) {
	// Put in the first 4 bytes the size of the value
	binary.LittleEndian.PutUint32(s.headerBuffer, uint32(len))
	s.copy(s.headerBuffer, headerEntrySize)
	s.copy(data, len)
}

func (s *cacheShard) copy(data []byte, len int) {
	// Using the tail to keep the order to write in the array
	s.tail += copy(s.array[s.tail:], data[:len])
}

func (s *cacheShard) get(key string, hashedKey uint64) ([]byte, error) {
	s.lock.RLock()
	itemIndex := int(s.items[hashedKey])
	if itemIndex == 0 {
		s.lock.RUnlock()
		return nil, errors.New("key not found")
	}

	// Read the first 4 bytes after the index, remember these 4 bytes have the size of the value, so
	// you can use this to get the size and get the value in the array using index+blockSize to know until what point
	// you need to read
	blockSize := int(binary.LittleEndian.Uint32(s.array[itemIndex : itemIndex+headerEntrySize]))
	entry := s.array[itemIndex+headerEntrySize : itemIndex+headerEntrySize+blockSize]
	s.lock.RUnlock()
	return readEntry(entry), nil
}

func readEntry(data []byte) []byte {
	dst := make([]byte, len(data))
	copy(dst, data)

	return dst
}

func wrapEntry(entry []byte) []byte {
	// You can put more information like a timestamp if you want.
	blobLength := len(entry)
	blob := make([]byte, blobLength)
	copy(blob, entry)
	return blob
}
