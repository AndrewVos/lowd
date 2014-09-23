package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
)

type Timer struct {
	Current int
	Running bool
}

func (t *Timer) Start() {
	if t.Running {
		return
	}
	go func() {
		t.Running = true
		for {
			t.Current += 1
			if t.Running == false {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

func (t *Timer) Stop() {
	t.Running = false
}

func record() {
	timer := &Timer{}
	defer timer.Stop()

	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if timer.Running == false {
			timer.Start()
		}
		if strings.HasPrefix(req.URL.Host, "-") {
			req.URL.Scheme = "https"

			newHost := req.URL.Host[1:]
			req.URL.Host = newHost
			req.Host = newHost
			req.Header.Set("Host", newHost)
		}
		err := storeRequest(timer.Current, req)
		if err != nil {
			log.Fatal("Error storing request:", err)
		}

		return req, nil
	})
	// proxy.Verbose = true
	log.Fatal(http.ListenAndServe(":8090", proxy))
}

func test() {
}

func main() {
	if len(os.Args) == 1 {
		fmt.Println("Usage: lowd [record|test]")
		return
	} else if os.Args[1] == "record" {
		record()
	} else if os.Args[1] == "test" {
		test()
	}
}

type Request struct {
	Time   int
	URL    string
	Method string
	Header map[string][]string
	Body   string
}

type StringReadCloser struct {
	reader io.Reader
}

func NewStringReadCloser(value string) *StringReadCloser {
	return &StringReadCloser{strings.NewReader(value)}
}

func (r *StringReadCloser) Read(b []byte) (n int, err error) {
	return r.reader.Read(b)
}

func (r *StringReadCloser) Close() error {
	return nil
}

func storeRequest(requestTime int, request *http.Request) error {
	body, err := ioutil.ReadAll(request.Body)
	request.Body.Close()
	request.Body = NewStringReadCloser(string(body))
	jsonRequest := Request{
		Time:   requestTime,
		URL:    request.URL.String(),
		Method: request.Method,
		Header: request.Header,
		Body:   string(body),
	}
	// record headers, cookies, body, request time
	b, err := json.Marshal(jsonRequest)
	if err != nil {
		return err
	}
	file, err := os.OpenFile("output.txt", os.O_CREATE|os.O_RDWR|os.O_APPEND, 0660)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(b)
	if err != nil {
		return err
	}
	_, err = file.WriteString("\n")
	if err != nil {
		return err
	}

	return nil
}
