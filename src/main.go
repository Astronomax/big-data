package main

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/go-chi/chi"
	"github.com/gofrs/uuid"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

//go:embed index.html
var content embed.FS

type txn struct {
	Source  string
	Id      int64
	Payload string
}

var snapshot []byte
var vclock map[string]int64
var replication []chan txn
var wal []txn
var txnQueue chan txn
var replicationMutex sync.Mutex
var snapshotMutex sync.Mutex
var port *int
var config *string
var source *string

func connectToUpstreams() { //connect to upstreams
	file, err := os.Open(*config)
	if err != nil {
		log.Println("Couldn't open replication config file")
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		addr := scanner.Text()
		go func(addr string) { //websocket client routine
			var url = fmt.Sprintf("ws://%s/ws", addr)
			for {
				func() {
					log.Println("Client: Trying to connect to upstream")
					c, _, err := websocket.Dial(context.Background(), url, nil)
					if err != nil {
						log.Println("Client: Error occured while connecting to websocket")
						return
					}
					defer c.Close(websocket.StatusInternalError, "Error occured while reading")
					log.Println("Client: Succesfully connected to websocket")
					for {
						log.Println("Client: Waiting transactions from upstream")
						var t txn
						err := wsjson.Read(context.Background(), c, &t)
						if err != nil {
							log.Println("Client: Error occured while waiting transactions")
							return
						}
						log.Println("Client: Successfully got transaction from upstream")
						txnQueue <- t
					}
				}()
				time.Sleep(10 * time.Second)
			}
		}(addr)
	}
	if err := scanner.Err(); err != nil {
		log.Println(err)
	}
}

func applyTxns() {
	for {
		t := <-txnQueue
		if t.Source == *source && t.Id == -1 {
			t.Id = vclock[*source]
		}
		patch, err := jsonpatch.DecodePatch([]byte(t.Payload))
		if err != nil {
			log.Println("Appplier: Error occured while decoding patch")
			continue
		}
		patchedSnapshot, err := patch.Apply(snapshot)
		if err != nil {
			log.Println("Appplier: Error occured while applying patch")
			continue
		}
		snapshotMutex.Lock()
		if t.Id >= vclock[t.Source] {
			snapshot = patchedSnapshot
			wal = append(wal, t)
			vclock[t.Source] = t.Id + 1

			replicationMutex.Lock()
			log.Println("Appplier: Sharing transaction with replicas")
			for _, replicaChan := range replication {
				replicaChan <- t
			}
			replicationMutex.Unlock()
		}
		snapshotMutex.Unlock()
	}
}

func main() {
	var err error
	uuid, err := uuid.NewV4()
	if err != nil {
		log.Println("Error occured while generating uuid")
		return
	}
	defaultSource := uuid.String()

	var bodyBytes []byte
	snapshot = []byte("{}")
	vclock = make(map[string]int64)
	replication = make([]chan txn, 0)
	wal = make([]txn, 0)
	txnQueue = make(chan txn)
	port = flag.Int("port", 80, "port")
	config = flag.String("config", "replication.txt", "config")
	source = flag.String("source", defaultSource, "source")
	flag.Parse()

	go connectToUpstreams()
	go applyTxns() //applier

	addr := fmt.Sprintf("localhost:%d", *port)

	m := chi.NewRouter()
	m.Handle("/", http.FileServer(http.FS(content)))
	m.Get("/vclock", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		bytes, err := json.Marshal(vclock)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Error occured while marshalling vclock"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(bytes)
	})
	m.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
			OriginPatterns:     []string{"*"},
		})
		if err != nil {
			log.Println("Server: Error while accepting websocket connection")
			return
		}
		defer c.Close(websocket.StatusInternalError, "Error occured while writing")
		log.Println("Server: New websocket connection")
		var replicaChan = make(chan txn, 239)

		snapshotMutex.Lock()
		log.Println("Server: Sending journal to newly connected replica")
		for _, transaction := range wal {
			err := wsjson.Write(context.Background(), c, transaction)
			if err != nil {
				log.Println("Server: Error occured while writing to websocket")
				return
			}
		}
		replicationMutex.Lock()
		replication = append(replication, replicaChan)
		replicationMutex.Unlock()
		snapshotMutex.Unlock()

		for transaction := range replicaChan {
			log.Println("Server: Sending transaction to replica")
			err := wsjson.Write(context.Background(), c, transaction)
			if err != nil {
				log.Println("Server: Error occured while writing to websocker")
				return
			}
		}
	})
	m.Get("/get", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		snapshotMutex.Lock()
		w.Write(snapshot)
		snapshotMutex.Unlock()
	})
	m.Post("/replace", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err = ioutil.ReadAll(r.Body)
		w.Header().Add("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Error occured while reading body"))
			return
		}
		txnQueue <- txn{Source: *source, Id: -1, Payload: string(bodyBytes)}
		w.WriteHeader(http.StatusOK)
	})
	log.Fatal(http.ListenAndServe(addr, m))
}
