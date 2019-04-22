package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/donniet/cec"
)

type urlFlag struct {
	URL *url.URL
}

func (u *urlFlag) String() string {
	if u.URL == nil {
		return ""
	}
	return u.URL.String()
}
func (u *urlFlag) Set(s string) (err error) {
	u.URL, err = url.Parse(s)
	return
}

type durationFlag time.Duration

func (f *durationFlag) String() string {
	return time.Duration(*f).String()
}

func (f *durationFlag) Set(s string) error {
	if d, err := time.ParseDuration(s); err != nil {
		return err
	} else {
		*f = durationFlag(d)
	}
	return nil
}

var (
	cecName       = "mirror"
	deviceName    = "0"
	mirrorAPI     urlFlag
	sleepDuration = durationFlag(10 * time.Minute)
	listenAddr    = ":8080"
)

func init() {
	flag.Var(&mirrorAPI, "mirrorURL", "url of smart mirror API")
	flag.StringVar(&cecName, "cecName", cecName, "cec name")
	flag.StringVar(&deviceName, "deviceName", deviceName, "cec device name")
	flag.Var(&sleepDuration, "sleep", "time before display goes to sleep")
	flag.StringVar(&listenAddr, "listen", listenAddr, "address to listen on")
}

type motionMessages struct {
	Motion struct {
		Detections []struct {
			DateTime time.Time `json:"dateTime"`
		} `json:"detections"`
		SleepDuration string `json:"sleepDuration"`
	} `json:"motion"`
	Display struct {
		PowerStatus string `json:"powerStatus"`
	} `json:"display"`
}

type request struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body"`
}

func sendPowerStatus(status string) error {
	b, err := json.Marshal(status)
	if err != nil {
		log.Fatal("error marshalling string: %v", err)
	}
	r, err := http.Post(mirrorAPI.String(), "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}

	if r.StatusCode != 200 {
		b, err := ioutil.ReadAll(r.Body)

		if err != nil {
			return err
		}

		return fmt.Errorf("error from api: %s", string(b))
	}

	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	// var msg motionMessages

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	conn, err := cec.Open(cecName, deviceName)
	if err != nil {
		log.Fatal(err)
		return
	}

	commands := make(chan *cec.Command)
	conn.Commands = commands

	fromService := make(chan bool)

	status := "on"

	if err := conn.PowerOn(0); err != nil {
		log.Printf("error powering on %v", err)
	} else if err := sendPowerStatus("on"); err != nil {
		log.Printf("error sending power status %v", err)
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		readStatus := ""

		switch r.Method {
		case http.MethodGet:
			if b, err := json.Marshal(status); err != nil {
				http.Error(w, "error marshalling data", http.StatusInternalServerError)
				log.Fatal(err)
			} else {
				w.Header().Set("Content-Type", "application/json")
				if _, err = w.Write(b); err != nil {
					log.Printf("error writing response, %v", err)
				}
			}
		case http.MethodPost:
			if b, err := ioutil.ReadAll(r.Body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			} else if err = json.Unmarshal(b, &readStatus); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			switch readStatus {
			case "on":
				fromService <- true
			case "standby":
				fromService <- false
			default:
				http.Error(w, "invalid status value", http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
	}

	s := &http.Server{
		Addr:    listenAddr,
		Handler: http.HandlerFunc(handler),
	}
	go s.ListenAndServe()
	defer s.Close()
	// http.ListenAndServe(listenAddr, http.Handler(handler))

	sleeper := time.NewTimer(time.Duration(math.MaxInt64))

	checker := time.NewTicker(1 * time.Minute)

eventLoop:
	for {
		err = nil

		select {
		case cmd := <-commands:
			var err error
			switch cmd.Operation {
			case "STANDBY":
				sleeper.Reset(time.Duration(math.MaxInt64))
				status = "standby"
			case "ROUTING_CHANGE":
				sleeper.Reset(time.Duration(sleepDuration))
				status = "on"
			}
			if err = sendPowerStatus(status); err != nil {
				log.Printf("error sending status: %v", err)
			}
		case <-sleeper.C:
			sleeper.Reset(time.Duration(math.MaxInt64))
			conn.Standby(0)
			status = "standby"

			if err = sendPowerStatus("standby"); err != nil {
				log.Printf("error sending status: %v", err)
			}
		case s := <-fromService:
			if s && status != "on" {
				conn.PowerOn(0)
				status = "on"
				err = sendPowerStatus(status)
			} else if !s && status != "standby" {
				conn.Standby(0)
				status = "standby"
				err = sendPowerStatus(status)
			}

			if err != nil {
				log.Printf("error sending status: %v", err)
			}
		case <-checker.C:
			readStatus := conn.GetDevicePowerStatus(0)
			if readStatus != status {
				if status == "on" {
					conn.PowerOn(0)
				} else if status == "standby" {
					conn.Standby(0)
				}
			}
		case <-interrupt:
			break eventLoop
		}
	}
}
