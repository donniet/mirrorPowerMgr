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

var (
	cecName      = "mirror"
	deviceName   = "0"
	websocketURL urlFlag
)

func init() {
	flag.Var(&websocketURL, "websocketURL", "websocket url of smart mirror")
	flag.StringVar(&cecName, "cecName", cecName, "cec name")
	flag.StringVar(&deviceName, "deviceName", deviceName, "cec device name")
}

type motionMessages struct {
	Motion struct {
		Detections []struct {
			DateTime time.Time `json:"dateTime"`
		} `json:"detections"`
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

		conn, err := cec.Open(cecName, deviceName)

		if err != nil {
			log.Fatal(err)
			return
		}

		commands := make(chan *cec.Command)
		conn.Commands = commands

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
					log.Printf("unmarshal error %v", err)
					continue
				}

				if len(msg.Motion.Detections) > 0 {
					log.Printf(msg.Motion.Detections[0].DateTime.String())
				}
			}
		}()

		sendPowerStatus := func(status string) error {
			req := request{}
			req.Method = "POST"
			req.Path = "display/powerStatus"
			req.Body = status

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
				switch cmd.Operation {
				case "STANDBY":
					if err := sendPowerStatus("standby"); err != nil {
						log.Printf("error sending status: %v", err)
					}
				case "ROUTING_CHANGE":
					if err := sendPowerStatus("on"); err != nil {
						log.Printf("error sending status: %v", err)
					}
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
