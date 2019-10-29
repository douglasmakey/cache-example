# How BigCache avoids expensive GC cycles and speeds up concurrent access

A few days ago, I read an article about BigCache and I was interested to know how they avoided these 2 problems:

* concurrent access
* expensive GC cycles

I went to their repository and read the code to understand how they achieved it. I think it's amazing so I would like to share it with you.

*'Fast, concurrent, evicting in-memory cache written to keep big number of entries without impact on performance. BigCache keeps entries on heap but omits GC for them. To achieve that operations on bytes arrays take place, therefore entries (de)serialization in front of the cache will be needed in most use cases.'*

[BigCache](https://github.com/allegro/bigcache)

## Concurrent access

Surely you will need concurrent access, either your program uses goroutines, or you have an HTTP server that allocates goroutines for each request. The most common approach to achieve it would be to use sync.RWMutex in front of the cache access function to ensure that only one goroutine could modify it at a time, but if you use this approach and other goroutine try to make modifications in the cache, the second goroutine would be blocked until the first goroutine unlock the lock, causing undesirable contention periods. 


To solve this problem, they used shards, but what is a shard? A shard is a struct that contains its instance of the cache with a lock.

Then they use an array of N shards to distribute the data into them, so when you are going to put or get data from the cache, a shard for that data is chosen by a function that we will talk later, in this way the locks contention can be minimized, because each shard has its lock.


```go
type cacheShard struct {
	items        map[uint64]uint32
	lock         sync.RWMutex
	array        []byte
	tail         int
}
```

## Expensive GC cycles

```go
var map[string]Item
```

The most common pattern in a simple implementation of cache in Go is using a map to save the items, but if you are using a map the garbage collector (GC) will touch every single item of that map during the mark phase, this can be very expensive on the application performance when the map is very large.

*After go version 1.5, if you use a map without pointers in keys and values, the GC will omit its content.*

```go
var map[int]int
```

To avoid this, they used a map without pointers in keys and values, with this the GC will omit the entries in the map and use an array of bytes, where they can put the entry serialized in bytes, then they can store in the map the hashedkey like key and the index of the entry into the array like the value.


Using an array of bytes is a smart solution because it only adds one additional object to the mark phase. Since a byte array doesn’t have any pointers (other than the object itself), the GC can mark the entire object in O(1) time.


# Let's start coding

It will be a fairly simple implementation of cache, I avoided eviction, capacity and other things, the code will be simple just to demonstrate how they solved the problems I talked above.

First, the hasher this is a `copy & paste` from their repository, you can find the code [Here](https://github.com/allegro/bigcache/blob/master/fnv.go), it is a **Hasher which makes no memory allocations.**

hasher.go

```go
package main

// newDefaultHasher returns a new 64-bit FNV-1a Hasher which makes no memory allocations.
// Its Sum64 method will lay the value out in big-endian byte order.
// See https://en.wikipedia.org/wiki/Fowler–Noll–Vo_hash_function
func newDefaultHasher() fnv64a {
	return fnv64a{}
}

type fnv64a struct{}

const (
	// offset64 FNVa offset basis. See https://en.wikipedia.org/wiki/Fowler–Noll–Vo_hash_function#FNV-1a_hash
	offset64 = 14695981039346656037
	// prime64 FNVa prime value. See https://en.wikipedia.org/wiki/Fowler–Noll–Vo_hash_function#FNV-1a_hash
	prime64 = 1099511628211
)

// Sum64 gets the string and returns its uint64 hash value.
func (f fnv64a) Sum64(key string) uint64 {
	var hash uint64 = offset64
	for i := 0; i < len(key); i++ {
		hash ^= uint64(key[i])
		hash *= prime64
	}

	return hash
}
```



Second, the cache struct contains the logic to get the shards and functions get&set.


I talked above in the **Concurrent access** section about a function to choose a shard for the data, to achived this they use the hasher above to hash the key and with the hashedkey get a shard for the the key, to achived that they do a bitwise operation with `AND` operator, using a mask based on the size of shards to turn off certain bits to get a value into the range of shards.


```
hashedkey&mask

    0111
AND 1101  (mask)
  = 0101
```


cache.go

```go
package main

var minShards = 1024

type cache struct {
	shards []*cacheShard
	hash   fnv64a
}

func newCache() *cache {
	cache := &cache{
		hash:   newDefaultHasher(),
		shards: make([]*cacheShard, minShards),
	}
	for i := 0; i < minShards; i++ {
		cache.shards[i] = initNewShard()
	}

	return cache
}

func (c *cache) getShard(hashedKey uint64) (shard *cacheShard) {
	return c.shards[hashedKey&uint64(minShards-1)]
}

func (c *cache) set(key string, value []byte) {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	shard.set(hashedKey, value)
}

func (c *cache) get(key string) ([]byte, error) {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.get(key, hashedKey)
}
```

Finally, where the magic occurs, in each shard have an array of bytes `[]byte` and a map `map[uint64]uint32`. In the map, they put the index for each entry like value and in the array save the entry in bytes.

They use the tail to keep the index in the array of bytes.

shard.go

```go
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

```

main.go

```go
package main

import "fmt"

func main() {
	cache := newCache()
	cache.set("key", []byte("the value"))

	value, err := cache.get("key")
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println(string(value))
	// OUTPUT:
	// the value
}

```