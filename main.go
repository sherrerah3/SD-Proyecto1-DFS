package main

import (
	"net/http"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /apple", apple)
	mux.HandleFunc("POST /banana", banana)
	mux.HandleFunc("DELETE /watermelon", watermelon)

	http.ListenAndServe(":8080", mux)
}

// -------------------------------------

// dest < src      : ResponseWriter comes before Request
// r *http.Request : mutable pointer to the request

func apple(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("apple"))
}

func banana(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("banana"))
}

func watermelon(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("watermelon"))
}
