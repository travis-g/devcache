package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/mux"
	cache "github.com/patrickmn/go-cache"
)

var (
	// Cache is the server-wide cache of previous requests.
	Cache *cache.Cache

	flagURL  string
	flagTTL  time.Duration
	flagAddr string
)

type server struct {
	router *mux.Router
}

// handleRequest simply pulls the path from the request out of the Cache. This
// handler is run after the caching middleware, so if somehow what we're looking
// for isn't cached there's been an internal issue.
func handleRequest(w http.ResponseWriter, r *http.Request) {
	response, found := Cache.Get(r.RequestURI)
	if !found {
		http.Error(w, "resource not found in cache", http.StatusInternalServerError)
		return
	}
	w.Write(response.([]byte))
	return
}

// trims and formats excess spacing of JSON bodies
func jsonMinify(data *[]byte) error {
	tmp := map[string]interface{}{}
	err := json.Unmarshal(*data, &tmp)
	if err != nil {
		return err
	}
	min, err := json.Marshal(tmp)
	if err != nil {
		return err
	}
	*data = min
	return nil
}

// cachingMiddleware checks to see if the desired request is present in the
// cache and fetches the data from the real API if necessary.
func cachingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.RequestURI
		_, found := Cache.Get(path)
		if !found {
			log.Printf("path %s not cached! forwarding headers and fetching\n", path)
			req, err := http.NewRequest("GET", flagURL+path, nil)
			if err != nil {
				panic(err)
			}
			// forward the headers
			req.Header = r.Header

			Client := &http.Client{
				Timeout: time.Second * 10,
			}
			res, err := Client.Do(req)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Printf("%v\n", err)
				return
			}
			body, err := ioutil.ReadAll(res.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				log.Printf("%v\n", err)
				return
			}
			// trim out excess content/whitespace before saving
			jsonMinify(&body)

			log.Printf("caching data from %s\n", req.URL)
			Cache.Set(path, body, cache.DefaultExpiration)
		} else {
			log.Printf("data present in cache for %s\n", path)
		}
		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s\n", r.Method, r.RequestURI)
		next.ServeHTTP(w, r)
	})
}

func readCache(filePath string, cache *map[string]cache.Item) error {
	file, err := os.Open(filePath)
	if err == nil {
		decoder := gob.NewDecoder(file)
		err = decoder.Decode(cache)
	}
	file.Close()
	return err
}

func writeCache(filePath string, cache map[string]cache.Item) error {
	file, err := os.Create(filePath)
	if err == nil {
		encoder := gob.NewEncoder(file)
		encoder.Encode(cache)
	}
	file.Close()
	return err
}

func main() {
	flag.StringVar(&flagURL, "url", "http://localhost:8080/", "url to proxy requests against")
	flag.DurationVar(&flagTTL, "ttl", 24*time.Hour, "duration to cache requests for")
	flag.StringVar(&flagAddr, "addr", ":8000", "address/port to configure the server")
	flag.Parse()

	items := new(map[string]cache.Item)
	err := readCache("./cache.gob", items)
	if err == nil {
		Cache = cache.NewFrom(flagTTL, flagTTL, *items)
		log.Printf("loaded cache (%d items)", Cache.ItemCount())
	} else {
		log.Printf("error loading cache: %s", err)
		Cache = cache.New(flagTTL, flagTTL)
	}

	handler := http.HandlerFunc(handleRequest)
	http.Handle("/", loggingMiddleware(cachingMiddleware(handler)))

	go func() {
		if err := http.ListenAndServe(flagAddr, nil); err != nil {
			log.Println(err)
		}
	}()

	log.Printf("server listening on %s, forwarding to %s", flagAddr, flagURL)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	<-c
	log.Println("shutting down")
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = writeCache("./cache.gob", Cache.Items())
	if err != nil {
		log.Printf("error writing cache: %s", err)
	}
	log.Printf("cache saved")
	os.Exit(0)
}
