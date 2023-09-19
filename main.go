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
	parallelism   = &atomic.Int64{} // parallelism
	rate          = &atomic.Int64{} // max-rate per worker
	input         = make(chan string)
	workers       = &atomic.Int64{}
	wg            = sync.WaitGroup{}
	perTryTimeout = 10 * time.Second
	retries       = 3
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
			if true &&
				// usage, to control parallelism, write: p=100
				!setting("p", "parallelism", text, atomicIntSetting{parallelism}) &&
				// usage, to control the max-rate per worker, set to 16/s, write: r=16
				!setting("r", "rate", text, atomicIntSetting{rate}) &&
				// usage, to control the per try timeout, write: t=10s
				!setting("t", "timeout", text, durationSetting{&perTryTimeout}) &&
				// usage, to control the retries, write: c=3
				!setting("c", "retries", text, intSetting{&retries}) {
				log.Println("unknown command", text)
			}
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
	var last time.Time
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
			// enforce max speed
			if time.Since(last) < time.Second/time.Duration(workRate) {
				time.Sleep(time.Second/time.Duration(workRate) - time.Since(last))
			}
			last = time.Now()

			resp, err := doReq(ctx, url)
			if err != nil {
				fmt.Println(url, errors.Wrap(err, "http"))
			} else {
				fmt.Println(url, resp.StatusCode)
			}
		}
	}
}

func doReq(ctx context.Context, url string) (resp *http.Response, err error) {
	for i := retries; i > 0; i-- {
		rctx, rcancel := context.WithTimeout(ctx, perTryTimeout)
		defer rcancel()
		req, _ := http.NewRequestWithContext(rctx, "GET", url, nil)
		resp, err = http.DefaultClient.Do(req)
		if err == nil {
			return resp, nil
		}
	}
	return
}

func setting(short, name, value string, storage storage) bool {
	if rest := strings.TrimPrefix(value, short+"="); strings.HasPrefix(value, short+"=") {
		if err := storage.Store(rest); err != nil {
			log.Println(err)
			return false
		} else {
			log.Printf("Set %s to %s", name, rest)
			return true
		}
	}
	return false
}

type storage interface {
	Store(string) error
}

type atomicIntSetting struct {
	*atomic.Int64
}

func (a atomicIntSetting) Store(val string) (err error) {
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return err
	}
	a.Int64.Store(i)
	return
}

type durationSetting struct {
	*time.Duration
}

func (d durationSetting) Store(val string) (err error) {
	*d.Duration, err = time.ParseDuration(val)
	return
}

type intSetting struct {
	*int
}

func (d intSetting) Store(val string) (err error) {
	*d.int, err = strconv.Atoi(val)
	return
}
