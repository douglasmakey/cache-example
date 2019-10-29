package main

import (
	"bytes"
	"fmt"
	"testing"
)

func Test_cache_set_and_get(t *testing.T) {
	tests := []struct {
		key   string
		value []byte
	}{
		{key: "mykey", value: []byte("the value")},
		{key: "otherkey", value: []byte("other value")},
		{key: "secret", value: []byte("value")},
	}

	cache := newCache()
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			cache.set(tt.key, tt.value)
			value, err := cache.get(tt.key)
			if err != nil {
				t.Error(err)
			}

			if bytes.Compare(value, tt.value) != 0 {
				t.Errorf("got %s expected %s", string(value), string(tt.value))
			}

		})
	}
}

func BenchmarkCache_Set(b *testing.B) {
	cache := newCache()
	for i := 0; i < 100; i++ {
		cache.set(fmt.Sprintf("mykey_%d", i), []byte("value"))
	}
}
