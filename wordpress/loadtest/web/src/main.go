package main

import (
	"fmt"
	"log"
	"net/http"
)

func requestHandler(res http.ResponseWriter, req *http.Request) {
	fmt.Fprint(res, "Everything's Gonna Be Alright!")
}

func main() {
	// register requestHandler to incoming requests for "/"
	http.HandleFunc("/", requestHandler)

	// run http server on the port 8080
	log.Fatal(http.ListenAndServe(":8080", nil))
}
