package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

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

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	// conn, err := cec.Open(cecName, deviceName)

	// if err != nil {
	// 	log.Fatal(err)
	// 	return
	// }

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

		for {
			select {
			case <-done:
				break
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
