package main

import (
	"flag"
	"log"
	"net/http"
)

var (
	listen string
)

func main() {
	flag.StringVar(&listen, "listen", ":8125", "Listen address")
	flag.Parse()

	log.Println("listening ", listen)
	err := http.ListenAndServe(listen, http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Write([]byte("Hello, World!"))
	}))

	if err != nil {
		panic(err)
	}
}
