package main

import (
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

var runnerPort string

func main() {
	currentTime := time.Now()
	fileName := "logs/" + currentTime.Format("2006-01-02_15-04-05") + ".txt"

	os.Mkdir("logs", 0777)
	file, err := os.Create(fileName)
	if err != nil {
		return
	}
	defer file.Close()

	args := os.Args

	_, err = file.WriteString(strings.Join(args, " ") + "\n")
	if err != nil {
		return
	}

	portIndex := getPortIndex(args)
	proxyPort := "8080"
	if portIndex > 0 {
		proxyPort = args[portIndex]
	}

	addr := "127.0.0.1:" + proxyPort
	go func() {
		http.HandleFunc("/", handle)
		err = http.ListenAndServe(addr, nil)
	}()

	port := 0
	if a, err := net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			port = l.Addr().(*net.TCPAddr).Port
			err = l.Close()
		}
	}
	if port == 0 {
		port = rand.Intn(65535-49152) + 49152 // get a random port in the ephemeral range
	}

	runnerPort = strconv.Itoa(port)

	if portIndex > 0 {
		args[portIndex] = runnerPort
	} else {
		args = append(args, "--port", runnerPort)
	}

	cmd := exec.Command("./ollama-real", args[1:]...)
	cmd.Stdout = file
	cmd.Stderr = file
	err = cmd.Start()
	err = cmd.Wait()
}

func getPortIndex(args []string) int {
	for i, arg := range args {
		if arg == "--port" && i+1 < len(args) {
			return i + 1
		}
	}
	return 0
}

func handle(w http.ResponseWriter, r *http.Request) {
	file, err := os.OpenFile("logs/requests.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()

	_, err = file.WriteString("START\n")
	if err != nil {
		return
	}

	targetURL := "http://localhost:" + runnerPort + r.URL.Path

	newReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Failed to create new request", http.StatusInternalServerError)
		return
	}
	newReq.Header = r.Header

	client := &http.Client{}
	resp, err := client.Do(newReq)
	if err != nil {
		http.Error(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

	_, err = file.WriteString("END\n")
	if err != nil {
		return
	}
}
