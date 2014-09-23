package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"

	"github.com/AndrewVos/colour"
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
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if timer.Running == false {
			timer.Start()
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
	file, err := os.Open("output.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var storedRequests []Request
	for scanner.Scan() {
		var request Request
		err := json.Unmarshal([]byte(scanner.Text()), &request)
		if err != nil {
			log.Fatal(err)
		}
		storedRequests = append(storedRequests, request)
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	cookieJar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: cookieJar,
	}

	timer := &Timer{}
	timer.Start()
	defer timer.Stop()
	for _, storedRequest := range storedRequests {
		for {
			if storedRequest.Time <= timer.Current {
				fmt.Printf(colour.Yellow("%v %v\n"), storedRequest.Method, storedRequest.URL)
				request, err := http.NewRequest(storedRequest.Method, storedRequest.URL, strings.NewReader(storedRequest.Body))
				if err != nil {
					log.Fatal(err)
				}
				response, err := client.Do(request)
				if err != nil {
					log.Println("Error during request:", err)
				}
				fmt.Printf("%+v\n", response)
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
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
