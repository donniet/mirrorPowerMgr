package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/donniet/cec"
	"github.com/gorilla/websocket"
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
	websocketURL  urlFlag
	sleepDuration = durationFlag(10 * time.Minute)
)

func init() {
	flag.Var(&websocketURL, "websocketURL", "websocket url of smart mirror")
	flag.StringVar(&cecName, "cecName", cecName, "cec name")
	flag.StringVar(&deviceName, "deviceName", deviceName, "cec device name")
	flag.Var(&sleepDuration, "sleep", "time before display goes to sleep")
}

type motionMessages struct {
	Motion struct {
		Detections []struct {
			DateTime time.Time `json:"dateTime"`
		} `json:"detections"`
		SleepDuration string `json:"sleepDuration"`
	} `json:"motion"`
}

type request struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   string `json:"body"`
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	var msg motionMessages

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	sleeper := NewSleeper(time.Duration(sleepDuration))
	defer sleeper.Close()

	conn, err := cec.Open(cecName, deviceName)
	if err != nil {
		log.Fatal(err)
		return
	}

	commands := make(chan *cec.Command)
	conn.Commands = commands

	// lastMotion := time.Time{}

	for {
		client, _, err := websocket.DefaultDialer.Dial(websocketURL.String(), nil)

		if err != nil {
			log.Printf("error connecting, retrying in 5sec: %v", err)

			select {
			case <-interrupt:
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		done := make(chan struct{})

		// reader
		go func() {
			defer close(done)

			for {
				_, message, err := client.ReadMessage()
				if err != nil {
					log.Printf("read error: %v", err)
					return
				}

				if err := json.Unmarshal(message, &msg); err != nil {
					log.Printf("unmarshal error %v - message %s", err, string(message))
					continue
				}

				if len(msg.Motion.Detections) > 0 {
					log.Printf(msg.Motion.Detections[0].DateTime.String())

					if time.Now().Sub(msg.Motion.Detections[0].DateTime) < sleeper.Timeout {
						sleeper.On()
					}
				}
				if msg.Motion.SleepDuration != "" {
					if d, err := time.ParseDuration(msg.Motion.SleepDuration); err != nil {
						log.Printf("error parsing duration: %s", msg.Motion.SleepDuration)
					} else {
						sleeper.Timeout = d
					}
				}
			}
		}()

		sendPowerStatus := func(status string) error {
			req := request{
				Method: "POST",
				Path:   "/display/powerStatus",
				Body:   status,
			}
			if b, err := json.Marshal(req); err != nil {
				log.Fatal(err)
				return nil
			} else {
				return client.WriteMessage(websocket.TextMessage, b)
			}
		}

		if err := conn.PowerOn(0); err != nil {
			log.Printf("error powering on %v", err)
		} else if err := sendPowerStatus("on"); err != nil {
			log.Printf("error sending power status %v", err)
		}

		for {
			select {
			case <-done:
				break
			case cmd := <-commands:
				var err error
				switch cmd.Operation {
				case "STANDBY":
					err = sendPowerStatus("standby")
					sleeper.Sleep()
				case "ROUTING_CHANGE":
					err = sendPowerStatus("on")
					sleeper.On()
				}
				if err != nil {
					log.Printf("error sending status: %v", err)
				}
			case sleepPower := <-sleeper.C:
				var err error
				if sleepPower {
					conn.PowerOn(0)
					// send the power status, which could be a duplicate
					err = sendPowerStatus("on")
				} else {
					conn.Standby(0)
					err = sendPowerStatus("standby")
				}
				if err != nil {
					log.Printf("error sending status: %v", err)
				}
			case <-interrupt:
				// Cleanly close the connection by sending a close message and then
				// waiting (with timeout) for the server to close the connection.
				err := client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				if err != nil {
					log.Println("write close:", err)
					return
				}
				select {
				case <-done:
				case <-time.After(time.Second):
				}
				return
			}
		}
	}
}
