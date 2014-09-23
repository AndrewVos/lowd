package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/elazarl/goproxy"
)

func record() {
	proxy := goproxy.NewProxyHttpServer()
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if strings.HasPrefix(req.URL.Host, "-") {
			req.URL.Scheme = "https"

			newHost := req.URL.Host[1:]
			req.URL.Host = newHost
			req.Host = newHost
			req.Header.Set("Host", newHost)
			for k, v := range req.Header {
				fmt.Println(k, v)
			}
			// record headers, cookies, body, request time
		}

		return req, nil
	})
	proxy.Verbose = true
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
