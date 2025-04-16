package main

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

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

	var wg sync.WaitGroup
	wg.Add(1)

	addr := "127.0.0.1:" + proxyPort
	go func() {
		defer wg.Done()
		http.HandleFunc("/", handle)
		err = http.ListenAndServe(addr, nil)
	}()

	cmd := exec.Command("kubectl", "apply", "-f", "kubernetes/pod.yaml")
	cmd.Stdout = file
	cmd.Stderr = file
	err = cmd.Start()
	err = cmd.Wait()

	cmd = exec.Command("kubectl", "apply", "-f", "kubernetes/service.yaml")
	cmd.Stdout = file
	cmd.Stderr = file
	err = cmd.Start()
	err = cmd.Wait()

	cmd = exec.Command("./ollama-proxy-watchdog")
	cmd.Stdout = file
	cmd.Stderr = file
	err = cmd.Start()

	wg.Wait()
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

	targetURL := "http://localhost:57156" + r.URL.Path

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
