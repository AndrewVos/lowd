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
	"net/url"
	"os"
	"sort"
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

func launchRecorder() {
	timer := &Timer{}
	defer timer.Stop()

	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if shouldRecordURL(req.URL) {
			if timer.Running == false {
				timer.Start()
			}

			fmt.Printf(colour.Yellow("%v %v\n"), req.Method, req.URL)
			err := storeRequest(timer.Current, req)
			if err != nil {
				log.Fatal("Error storing request:", err)
			}
		}

		return req, nil
	})

	// proxy.Verbose = true
	fmt.Printf("Starting recorder on %v\n", *port)

	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(*port), proxy))
}

func shouldRecordURL(url *url.URL) bool {
	if *whitelist == "" {
		return true
	}
	for _, host := range strings.Split(*whitelist, ",") {
		if strings.Contains(url.Host, host) {
			return true
		}
	}
	return false
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
	clientResults := make(chan ClientResult)

	for i := 0; i < *maximumClients; i++ {
		clientNumber += 1
		currentClients += 1
		go singleClientTest(clientNumber, clientResults, storedRequests)
	}

	startTime := time.Now()

	var allClientResults []ClientResult
	for clientResult := range clientResults {
		allClientResults = append(allClientResults, clientResult)
		clientNumber += 1
		currentClients -= 1
		fmt.Println(fmt.Sprintf(colour.Green("Client #%v"), clientResult.ClientNumber))
		for _, requestResult := range clientResult.Results {
			fmt.Println(colour.Yellow(requestResult.Title()))
			if requestResult.Error != nil {
				fmt.Printf(colour.Red("Error during request:\n%v\n"), err)
			}

			if *writeResponseHeaders {
				fmt.Println(requestResult.Headers)
			}
			if *writeResponseBody {
				fmt.Println(requestResult.Body)
			}
			if *writeResponseTime {
				fmt.Printf(colour.Blue("response time: %s\n"), requestResult.ResponseTime)
			}

		}
		fmt.Printf("Current clients: %v\n", currentClients)
		currentDuration := time.Since(startTime)
		if currentDuration.Seconds() >= *duration {
			fmt.Printf(colour.Blue("Waiting for %v clients to complete...\n"), currentClients)
			if currentClients == 0 {
				close(clientResults)
			}
			continue
		}
		currentClients += 1
		go singleClientTest(clientNumber, clientResults, storedRequests)
	}

	if *summary {
		printSummary(allClientResults)
	}
}

func printSummary(allClientResults []ClientResult) {
	fmt.Println("------------------")
	fmt.Println("------SUMMARY-----")
	fmt.Println("------------------")
	var allRequestResults RequestResults
	for _, clientResult := range allClientResults {
		for _, requestResult := range clientResult.Results {
			allRequestResults = append(allRequestResults, requestResult)
		}
	}
	sort.Sort(allRequestResults)
	fastestResponseTimes := map[string]time.Duration{}
	slowestResponseTimes := map[string]time.Duration{}
	for _, requestResult := range allRequestResults {
		if v, ok := fastestResponseTimes[requestResult.Title()]; ok {
			if requestResult.ResponseTime < v {
				fastestResponseTimes[requestResult.Title()] = requestResult.ResponseTime
			}
		} else {
			fastestResponseTimes[requestResult.Title()] = requestResult.ResponseTime
		}

		if v, ok := slowestResponseTimes[requestResult.Title()]; ok {
			if requestResult.ResponseTime > v {
				slowestResponseTimes[requestResult.Title()] = requestResult.ResponseTime
			}
		} else {
			slowestResponseTimes[requestResult.Title()] = requestResult.ResponseTime
		}
	}
	for title, value := range fastestResponseTimes {
		fastest := fmt.Sprintf(colour.Green("fastest: %s"), value)
		slowest := fmt.Sprintf(colour.Red("slowest: %s"), slowestResponseTimes[title])
		fmt.Printf(colour.Yellow("%v\n%v\n%v\n"), title, fastest, slowest)
	}
}

type RequestResults []RequestResult

func (r RequestResults) Len() int           { return len(r) }
func (r RequestResults) Swap(i, j int)      { r[i], r[j] = r[j], r[i] }
func (r RequestResults) Less(i, j int) bool { return r[i].ResponseTime < r[j].ResponseTime }

type ClientResult struct {
	ClientNumber int
	Results      RequestResults
}

type RequestResult struct {
	Method       string
	StatusCode   int
	URL          string
	Error        error
	Headers      string
	Body         string
	ResponseTime time.Duration
}

func (r RequestResult) Title() string {
	return fmt.Sprintf("%v %v %v", r.StatusCode, r.Method, r.URL)
}

func singleClientTest(clientNumber int, clientResults chan ClientResult, storedRequests []Request) {
	cookieJar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar: cookieJar,
	}

	timer := &Timer{}
	timer.Start()
	defer timer.Stop()

	clientResult := ClientResult{ClientNumber: clientNumber}

	for _, storedRequest := range storedRequests {
		for {
			if storedRequest.Time <= timer.Current {
				result := RequestResult{
					Method: storedRequest.Method,
					URL:    storedRequest.URL,
				}
				request, err := http.NewRequest(storedRequest.Method, storedRequest.URL, strings.NewReader(storedRequest.Body))
				if err != nil {
					log.Fatal(err)
				}
				start := time.Now()
				response, err := client.Do(request)
				if err != nil {
					result.Error = err
					break
				}

				result.StatusCode = response.StatusCode

				r, err := httputil.DumpResponse(response, false)
				if err != nil {
					log.Fatal(err)
				}
				result.Headers = strings.TrimSpace(string(r))
				b, err := ioutil.ReadAll(response.Body)
				if err != nil {
					log.Fatal(err)
				}
				result.Body = string(b)
				if response.Body != nil {
					response.Body.Close()
				}
				result.ResponseTime = time.Since(start)
				clientResult.Results = append(clientResult.Results, result)

				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	clientResults <- clientResult
}

var port *int
var whitelist *string
var writeResponseHeaders *bool
var writeResponseBody *bool
var writeResponseTime *bool
var summary *bool
var duration *float64
var maximumClients *int

func main() {
	record := flag.Bool("record", false, "start the proxy recorder")
	test := flag.Bool("test", false, "start a load test")
	coloursEnabled := flag.Bool("colour", true, "write output in colour")

	port = flag.Int("port", 8090, "when recording, the port to bind to")
	whitelist = flag.String("whitelist", "", "when recording, a comma seperated whitelist of url matches")
	writeResponseHeaders = flag.Bool("write-response-headers", false, "when running a load test, write the response headers out")
	writeResponseBody = flag.Bool("write-response-body", false, "when running a load test, write the response body out")
	writeResponseTime = flag.Bool("write-response-time", true, "when running a load test, write the response time for each request")
	summary = flag.Bool("summary", true, "when running a load test, display a summary at the end")
	duration = flag.Float64("duration", 300, "when running a load test, the duration in seconds")
	maximumClients = flag.Int("maximum-clients", 10, "when running a load test, the maximum amount of clients")

	flag.Parse()

	colour.Enabled = *coloursEnabled

	if !*record && !*test {
		flag.Usage()
	}
	if *record {
		launchRecorder()
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
