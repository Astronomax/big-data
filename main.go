package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"
	"os"
	"github.com/go-chi/chi/v5"
)

func main() {
	port := flag.Int("port", 80, "port")
	flag.Parse()

	addr := fmt.Sprintf("localhost:%d", *port)

	var mu sync.Mutex
	queue := make(chan string, 0)
	currentValue := "initial"

	f, err := os.OpenFile("log.txt", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	go func() {
		for {
			value := <-queue
			func() {
				mu.Lock()
				defer mu.Unlock()
				currentValue = value
				if _, err = f.WriteString(fmt.Sprintf("%s\n", value)); err != nil {
					panic(err)
				}
			}()
		}
	}()

	go func() {
		num := 0
		for {
			<-time.After(20 * time.Second)
			func() {
				mu.Lock()
				defer mu.Unlock()
				f, err := os.OpenFile(fmt.Sprintf("snapshot%d.txt", num), os.O_CREATE|os.O_RDWR, 0644)
				if err != nil {
					panic(err)
				}
				defer f.Close()
				if _, err = f.WriteString(currentValue); err != nil {
					panic(err)
				}
			}()
			num += 1
		}
	}()

	m := chi.NewRouter()
	m.Get("/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		mu.Lock()
		defer mu.Unlock()
		w.Write([]byte(currentValue))
	})
	m.Post("/replace", func(w http.ResponseWriter, r *http.Request) {
		jsonBytes, err := ioutil.ReadAll(r.Body)
		w.Header().Add("Content-Type", "plain/text; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("An Error Occured"))
			return
		}
		value := string(jsonBytes)
		select {
		case queue <- value:
			w.WriteHeader(http.StatusOK)
			w.Write(([]byte)("OK"))
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("A Timeout Occured"))
		}
	})
	log.Fatal(http.ListenAndServe(addr, m))
}