package main

import (
	"fmt"
	"log"
	"net/http"
	"encoding/json"
	"github.com/hpcloud/tail"
	"strings"
)

type Monitors struct {
	RequestCount200 int64 `json:"200"`
	RequestCount503 int64 `json:"503"`
	RequestCountOther int64 `json:"other"`
}

var monitors = Monitors{0, 0, 0}

func requestHandler(res http.ResponseWriter, req *http.Request) {
	res.Header().Set("Content-Type", "application/json")

	var jsonData []byte
	jsonData, err := json.Marshal(monitors)
	if err != nil {
		log.Println(err)
	}
	fmt.Println(string(jsonData))
	res.Write(jsonData)
}

func startServer() {
	// register requestHandler to incoming requests for "/"
	http.HandleFunc("/", requestHandler)

	// run http server on the port 80
	log.Fatal(http.ListenAndServe(":80", nil))
}

func main() {
	go startServer()

	t, err := tail.TailFile("/var/log/logparser/http-access.log", tail.Config{
		Follow: true,
		ReOpen: true})

	if err != nil {
		log.Println(err)
	}

	for line := range t.Lines {
		fmt.Println(line.Text)
		var searchPattern string = "\"GET / HTTP/1.1\" "
		var pos = strings.Index(line.Text, searchPattern)
		var startIdx = pos + len(searchPattern)
		responseCode := line.Text[startIdx:startIdx + 3]
		switch responseCode {
		case "200":
			monitors.RequestCount200++
		case "503":
			monitors.RequestCount503++
		default:
			monitors.RequestCountOther++
		}
	}
}
