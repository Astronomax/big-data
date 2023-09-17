package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	port := flag.Int("port", 80, "port")
	flag.Parse()

	addr := fmt.Sprintf("localhost:%d", *port)
	var jsonBytes []byte
	var err error

	mutex := make(chan struct{}, 1)

	m := chi.NewRouter()
	m.Get("/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		mutex <- struct{}{}
		_, _ = w.Write(jsonBytes)
		<-mutex
	})
	m.Post("/replace", func(w http.ResponseWriter, r *http.Request) {
		mutex <- struct{}{}
		jsonBytes, err = ioutil.ReadAll(r.Body)
		<-mutex
		w.Header().Add("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(([]byte)("Error occured while reading body"))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	log.Fatal(http.ListenAndServe(addr, m))
}
