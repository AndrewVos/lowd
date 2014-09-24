package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"os"
	"strconv"
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

func launchRecorder(port int) {
	timer := &Timer{}
	defer timer.Stop()

	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if timer.Running == false {
			timer.Start()
		}

		fmt.Printf(colour.Yellow("%v %v\n"), req.Method, req.URL)
		err := storeRequest(timer.Current, req)
		if err != nil {
			log.Fatal("Error storing request:", err)
		}

		return req, nil
	})

	// proxy.Verbose = true
	fmt.Printf("Starting recorder on %v\n", port)

	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(port), proxy))
}

func runLoadTest() {
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

	clientNumber := 0
	currentClients := 0
	results := make(chan string)

	for i := 0; i < *maximumClients; i++ {
		clientNumber += 1
		currentClients += 1
		go singleClientTest(clientNumber, results, storedRequests)
	}

	startTime := time.Now()

	for nextResult := range results {
		clientNumber += 1
		currentClients -= 1
		fmt.Println(nextResult)
		fmt.Printf("Current clients: %v\n", currentClients)
		currentDuration := time.Since(startTime)
		if currentDuration.Seconds() >= *duration {
			fmt.Printf(colour.Blue("Waiting for %v clients to complete...\n"), currentClients)
			if currentClients == 0 {
				close(results)
			}
			continue
		}
		currentClients += 1
		go singleClientTest(clientNumber, results, storedRequests)
	}
}

func singleClientTest(clientNumber int, results chan string, storedRequests []Request) {
	cookieJar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: cookieJar,
	}

	timer := &Timer{}
	timer.Start()
	defer timer.Stop()

	result := fmt.Sprintf(colour.Green("Client #%v\n"), clientNumber)

	for _, storedRequest := range storedRequests {
		for {
			if storedRequest.Time <= timer.Current {
				result += fmt.Sprintf(colour.Yellow("%v %v\n"), storedRequest.Method, storedRequest.URL)
				request, err := http.NewRequest(storedRequest.Method, storedRequest.URL, strings.NewReader(storedRequest.Body))
				if err != nil {
					log.Fatal(err)
				}
				start := time.Now()
				response, err := client.Do(request)
				if err != nil {
					result += fmt.Sprintf(colour.Red("Error during request:\n%v\n"), err)
					break
				}
				if *writeResponseHeaders {
					r, err := httputil.DumpResponse(response, false)
					if err != nil {
						log.Fatal(err)
					}
					result += strings.TrimSpace(string(r)) + "\n"
				}
				if *writeResponseBody {
					b, err := ioutil.ReadAll(response.Body)
					if err != nil {
						log.Fatal(err)
					}
					result += string(b) + "\n"
				}
				if response.Body != nil {
					response.Body.Close()
				}
				if *writeResponseTime {
					elapsed := time.Since(start)
					result += fmt.Sprintf(colour.Blue("response time: %s\n"), elapsed)
				}

				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	results <- result
}

var writeResponseHeaders *bool
var writeResponseBody *bool
var writeResponseTime *bool
var duration *float64
var maximumClients *int

func main() {
	record := flag.Bool("record", false, "start the proxy recorder")
	test := flag.Bool("test", false, "start a load test")
	coloursEnabled := flag.Bool("colour", true, "write output in colour")
	port := flag.Int("port", 8090, "when recording, the port to bind to")

	writeResponseHeaders = flag.Bool("write-response-headers", false, "when running a load test, write the response headers out")
	writeResponseBody = flag.Bool("write-response-body", false, "when running a load test, write the response body out")
	writeResponseTime = flag.Bool("write-response-time", true, "when running a load test write the response time for each request")
	duration = flag.Float64("duration", 300, "when running a load test, the duration in seconds")
	maximumClients = flag.Int("maximum-clients", 10, "when running a load test, the maximum amount of clients")

	flag.Parse()

	colour.Enabled = *coloursEnabled

	if !*record && !*test {
		flag.Usage()
	}
	if *record {
		launchRecorder(*port)
	} else if *test {
		runLoadTest()
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
