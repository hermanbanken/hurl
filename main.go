package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

var (
	parallelism = atomic.Int64{} // parallelism
	rate        = atomic.Int64{} // max-rate per worker
	input       = make(chan string)
	workers     = atomic.Int64{}
	wg          = sync.WaitGroup{}
)

func main() {
	log.SetFlags(0)
	log.SetOutput(os.Stderr)
	parallelism.Store(1)
	rate.Store(1)
	workers.Store(0)
	go startWorker() // single worker to start

	// routine to change settings live
	go func() {
		inScanner := bufio.NewScanner(os.Stdin)
		inScanner.Split(bufio.ScanLines)
		for inScanner.Scan() {
			text := strings.TrimSpace(inScanner.Text())
			// usage, to control parallelism, write: p=100
			if rest := strings.TrimPrefix(text, "p="); strings.HasPrefix(text, "p=") {
				p, err := strconv.ParseInt(rest, 10, 64)
				if err == nil {
					parallelism.Store(p)
					log.Printf("Set parallelism to %d", p)
					current := workers.Load()
					if p > current {
						for i := current; i < p; i++ {
							go startWorker()
						}
					}
				} else {
					log.Println(err)
				}
				continue
			}
			// usage, to control the max-rate per worker, set to 16/s, write: r=16
			if rest := strings.TrimPrefix(text, "r="); strings.HasPrefix(text, "r=") {
				r, err := strconv.ParseInt(rest, 10, 64)
				if err == nil {
					rate.Store(r)
					log.Printf("Set rate to %d/s", r)
				} else {
					log.Println(err)
				}
				continue
			}
			log.Println("unknown command", text)
		}
	}()

	// read urls from input file
	if len(os.Args) < 2 {
		log.Fatal("missing input file; usage: hurl <url-list-file>")
	}
	readFile, err := os.Open(os.Args[1])
	if err != nil {
		log.Println(err)
	}
	fileScanner := bufio.NewScanner(readFile)
	fileScanner.Split(bufio.ScanLines)
	for fileScanner.Scan() {
		text := strings.TrimSpace(fileScanner.Text())
		if text != "" {
			input <- text
		}
	}
	readFile.Close()
	close(input)

	wg.Wait()
}

func startWorker() {
	wg.Add(1)
	defer wg.Done()
	idx := workers.Add(1) - 1
	defer func() {
		workers.Add(-1)
	}()
	t := time.NewTicker(time.Second)
	workRate := rate.Load()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for {
		select {
		case <-t.C:
			workRate = rate.Load()
			if parallelism.Load() <= int64(idx) {
				return
			}
		case url, ok := <-input:
			if !ok {
				return
			}
			rctx, rcancel := context.WithTimeout(ctx, 10*time.Second)
			defer rcancel()
			req, _ := http.NewRequestWithContext(rctx, "GET", url, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Println(url, errors.Wrap(err, "http"))
			} else {
				fmt.Println(url, resp.StatusCode)
			}
			time.Sleep(time.Second / time.Duration(workRate))
		}
	}
}
