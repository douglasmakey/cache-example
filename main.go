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
}
